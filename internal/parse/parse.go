// Package parse turns a link into an idea draft: og:* and schema.org from the page,
// with a guard against fetching private addresses.
package parse

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"html"
	"io"
	"net"
	"net/http"
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"time"
)

type Result struct {
	Title       string
	Description string
	Image       string
	Address     string
	PriceRub    int64 // per person, rough
	SiteName    string
	Category    string // guess from host/type: food, culture, walk, sport, other
	StartsAt    *time.Time
	EndsAt      *time.Time
}

var urlRe = regexp.MustCompile(`https?://[^\s<>"']+`)

// FirstURL finds a link in a message ("yandex.ru/maps/…" without a scheme is accepted too).
func FirstURL(text string) string {
	if m := urlRe.FindString(text); m != "" {
		return strings.TrimRight(m, ".,);!?»")
	}
	for _, f := range strings.Fields(text) {
		if strings.Count(f, ".") >= 1 && strings.Contains(f, "/") && !strings.Contains(f, "@") {
			if u, err := url.Parse("https://" + f); err == nil && strings.Contains(u.Host, ".") {
				return strings.TrimRight("https://"+f, ".,);!?»")
			}
		}
	}
	return ""
}

// guarded dialer refuses loopback, private and link-local targets (SSRF).
func safeDialer() *net.Dialer {
	return &net.Dialer{Timeout: 5 * time.Second, Control: nil}
}

func isPrivate(ip net.IP) bool {
	return ip.IsLoopback() || ip.IsPrivate() || ip.IsLinkLocalUnicast() || ip.IsLinkLocalMulticast() || ip.IsUnspecified() || ip.IsMulticast()
}

var ErrBlocked = errors.New("parse: address not allowed")

func Client(allowPrivate bool) *http.Client {
	d := safeDialer()
	tr := &http.Transport{
		Proxy: http.ProxyFromEnvironment,
		DialContext: func(ctx context.Context, network, addr string) (net.Conn, error) {
			host, port, err := net.SplitHostPort(addr)
			if err != nil {
				return nil, err
			}
			ips, err := net.DefaultResolver.LookupIP(ctx, "ip", host)
			if err != nil {
				return nil, err
			}
			for _, ip := range ips {
				if !allowPrivate && isPrivate(ip) {
					return nil, ErrBlocked
				}
			}
			return d.DialContext(ctx, network, net.JoinHostPort(ips[0].String(), port))
		},
		TLSHandshakeTimeout: 5 * time.Second,
	}
	return &http.Client{Timeout: 6 * time.Second, Transport: tr, CheckRedirect: func(r *http.Request, via []*http.Request) error {
		if len(via) >= 4 {
			return errors.New("too many redirects")
		}
		return nil
	}}
}

// Fetch downloads at most 1 MB of the page and extracts what it can.
func Fetch(ctx context.Context, c *http.Client, link string) (Result, error) {
	u, err := url.Parse(link)
	if err != nil || (u.Scheme != "http" && u.Scheme != "https") {
		return Result{}, fmt.Errorf("parse: bad url")
	}
	req, _ := http.NewRequestWithContext(ctx, "GET", link, nil)
	req.Header.Set("User-Agent", "Mozilla/5.0 (compatible; VdvoemBot/1.0; +https://vdvoem.vershov.com)")
	req.Header.Set("Accept-Language", "ru,en;q=0.8")
	res, err := c.Do(req)
	if err != nil {
		return Result{Category: GuessCategory(u.Host, "")}, err
	}
	defer res.Body.Close()
	b, _ := io.ReadAll(io.LimitReader(res.Body, 1<<20))
	r := Extract(string(b))
	if r.Category == "" {
		r.Category = GuessCategory(u.Host, r.Title+" "+r.Description)
	}
	if r.Image != "" && !strings.HasPrefix(r.Image, "http") {
		if iu, err := u.Parse(r.Image); err == nil {
			r.Image = iu.String()
		}
	}
	return r, nil
}

var (
	metaRe  = regexp.MustCompile(`(?is)<meta\s+[^>]*>`)
	attrRe  = regexp.MustCompile(`(?is)(property|name|content|itemprop)\s*=\s*("([^"]*)"|'([^']*)')`)
	titleRe = regexp.MustCompile(`(?is)<title[^>]*>(.*?)</title>`)
	ldRe    = regexp.MustCompile(`(?is)<script[^>]+application/ld\+json[^>]*>(.*?)</script>`)
	priceRe = regexp.MustCompile(`(\d[\d\s\x{00a0}\x{202f}]{1,7})\s*(?:₽|руб)`)
)

func Extract(page string) Result {
	var r Result
	meta := map[string]string{}
	for _, m := range metaRe.FindAllString(page, 200) {
		var key, val string
		for _, a := range attrRe.FindAllStringSubmatch(m, -1) {
			v := a[3] + a[4]
			switch strings.ToLower(a[1]) {
			case "property", "name", "itemprop":
				key = strings.ToLower(v)
			case "content":
				val = v
			}
		}
		if key != "" && val != "" {
			if _, ok := meta[key]; !ok {
				meta[key] = html.UnescapeString(val)
			}
		}
	}
	pick := func(keys ...string) string {
		for _, k := range keys {
			if v := strings.TrimSpace(meta[k]); v != "" {
				return v
			}
		}
		return ""
	}
	r.Title = pick("og:title", "twitter:title")
	if r.Title == "" {
		if m := titleRe.FindStringSubmatch(page); m != nil {
			r.Title = strings.TrimSpace(html.UnescapeString(m[1]))
		}
	}
	r.Description = pick("og:description", "description", "twitter:description")
	r.Image = pick("og:image", "og:image:url", "twitter:image")
	r.SiteName = pick("og:site_name")
	r.Address = pick("place:location:address", "og:street-address", "business:contact_data:street_address")
	for _, m := range ldRe.FindAllStringSubmatch(page, 5) {
		ld(strings.TrimSpace(m[1]), &r)
	}
	if r.PriceRub == 0 {
		if m := priceRe.FindStringSubmatch(r.Description); m != nil {
			r.PriceRub = digits(m[1])
		}
	}
	r.Title = cleanTitle(r.Title, r.SiteName)
	if len([]rune(r.Title)) > 80 {
		r.Title = string([]rune(r.Title)[:80])
	}
	return r
}

func digits(s string) int64 {
	var b strings.Builder
	for _, c := range s {
		if c >= '0' && c <= '9' {
			b.WriteRune(c)
		}
	}
	n, _ := strconv.ParseInt(b.String(), 10, 64)
	return n
}

// cleanTitle drops « — Яндекс Карты», « | Афиша» tails.
func cleanTitle(t, site string) string {
	for _, sep := range []string{" — ", " | ", " - ", " · "} {
		if i := strings.LastIndex(t, sep); i > 0 {
			tail := strings.ToLower(t[i+len(sep):])
			if site != "" && strings.Contains(tail, strings.ToLower(site)) || strings.Contains(tail, "яндекс") || strings.Contains(tail, "2гис") ||
				strings.Contains(tail, "афиша") || strings.Contains(tail, "карты") || strings.Contains(tail, "отзывы") {
				t = t[:i]
			}
		}
	}
	return strings.TrimSpace(t)
}

func ld(raw string, r *Result) {
	var any interface{}
	if json.Unmarshal([]byte(raw), &any) != nil {
		return
	}
	var walk func(v interface{})
	walk = func(v interface{}) {
		switch x := v.(type) {
		case []interface{}:
			for _, e := range x {
				walk(e)
			}
		case map[string]interface{}:
			if g, ok := x["@graph"]; ok {
				walk(g)
			}
			typ := fmt.Sprint(x["@type"])
			if r.Title == "" {
				if n, ok := x["name"].(string); ok {
					r.Title = n
				}
			}
			switch {
			case strings.Contains(typ, "Restaurant") || strings.Contains(typ, "FoodEstablishment") || strings.Contains(typ, "Cafe") || strings.Contains(typ, "Bar"):
				r.Category = "food"
			case strings.Contains(typ, "Event") || strings.Contains(typ, "Museum") || strings.Contains(typ, "Theater") || strings.Contains(typ, "Movie"):
				r.Category = "culture"
			case strings.Contains(typ, "Park") || strings.Contains(typ, "TouristAttraction"):
				r.Category = "walk"
			case strings.Contains(typ, "Sports") || strings.Contains(typ, "Exercise"):
				r.Category = "sport"
			}
			if a, ok := x["address"]; ok && r.Address == "" {
				switch av := a.(type) {
				case string:
					r.Address = av
				case map[string]interface{}:
					r.Address = strings.Trim(fmt.Sprintf("%v", av["streetAddress"]), " <nil>")
				}
			}
			if p, ok := x["priceRange"].(string); ok && r.PriceRub == 0 {
				r.PriceRub = digits(strings.Split(p, "–")[0])
			}
			if off, ok := x["offers"].(map[string]interface{}); ok && r.PriceRub == 0 {
				r.PriceRub = digits(fmt.Sprint(off["price"]))
			}
			if s, ok := x["startDate"].(string); ok {
				if t, err := time.Parse(time.RFC3339, s); err == nil {
					r.StartsAt = &t
				} else if t, err := time.Parse("2006-01-02", s[:min(10, len(s))]); err == nil {
					r.StartsAt = &t
				}
			}
			if s, ok := x["endDate"].(string); ok {
				if t, err := time.Parse(time.RFC3339, s); err == nil {
					r.EndsAt = &t
				} else if t, err := time.Parse("2006-01-02", s[:min(10, len(s))]); err == nil {
					r.EndsAt = &t
				}
			}
			if img, ok := x["image"].(string); ok && r.Image == "" {
				r.Image = img
			}
		}
	}
	walk(any)
}

// GuessCategory from words in the host and text (used when the page says nothing).
func GuessCategory(host, text string) string {
	s := strings.ToLower(host + " " + text)
	has := func(ws ...string) bool {
		for _, w := range ws {
			if strings.Contains(s, w) {
				return true
			}
		}
		return false
	}
	switch {
	case has("ресторан", "кафе", "бар ", "бар,", "кофейн", "пицц", "суши", "хачапури", "ужин", "завтрак", "бистро", "гриль", "food", "restaurant", "cafe", "coffee"):
		return "food"
	case has("театр", "музей", "выставк", "концерт", "кино", "галере", "спектакл", "afisha", "афиша", "lecture", "лекци", "tickets", "билет"):
		return "culture"
	case has("парк", "прогулк", "набережн", "лес", "сад", "усадьб", "park", "trail"):
		return "walk"
	case has("каток", "бассейн", "скалодром", "йога", "бег", "велосипед", "лыж", "sport", "fitness", "боулинг", "падел", "теннис"):
		return "sport"
	case has("убрать", "помыть", "купить", "починить", "записаться", "оплатить", "постирать", "собрать", "разобрать"):
		return "home"
	}
	return "other"
}
