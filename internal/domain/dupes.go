package domain

import (
	"net/url"
	"sort"
	"strings"
	"unicode"
)

// Fingerprint: the same place gives the same string. URL → host+path without tracking params;
// otherwise the normalized title.
func Fingerprint(rawURL, title string) string {
	if rawURL != "" {
		if u, err := url.Parse(strings.TrimSpace(rawURL)); err == nil && u.Host != "" {
			host := strings.TrimPrefix(strings.ToLower(u.Host), "www.")
			host = strings.TrimPrefix(host, "m.")
			q := u.Query()
			keep := url.Values{}
			for k, v := range q {
				lk := strings.ToLower(k)
				if strings.HasPrefix(lk, "utm_") || lk == "from" || lk == "ref" || lk == "ll" || lk == "z" || lk == "si" {
					continue
				}
				keep[k] = v
			}
			keys := make([]string, 0, len(keep))
			for k := range keep {
				keys = append(keys, k)
			}
			sort.Strings(keys)
			var qs []string
			for _, k := range keys {
				qs = append(qs, k+"="+keep.Get(k))
			}
			fp := "u:" + host + strings.TrimRight(u.Path, "/")
			if len(qs) > 0 {
				fp += "?" + strings.Join(qs, "&")
			}
			return fp
		}
	}
	return "t:" + NormTitle(title)
}

// NormTitle lowercases, folds ё→е, drops punctuation and doubled spaces.
func NormTitle(s string) string {
	s = strings.ToLower(strings.ReplaceAll(strings.ReplaceAll(s, "ё", "е"), "Ё", "е"))
	var b strings.Builder
	space := false
	for _, r := range s {
		if unicode.IsLetter(r) || unicode.IsDigit(r) {
			b.WriteRune(r)
			space = false
		} else if !space && b.Len() > 0 {
			b.WriteRune(' ')
			space = true
		}
	}
	return strings.TrimSpace(b.String())
}

func trigrams(s string) map[string]bool {
	r := []rune(" " + NormTitle(s) + " ")
	out := map[string]bool{}
	for i := 0; i+3 <= len(r); i++ {
		out[string(r[i:i+3])] = true
	}
	return out
}

// Similarity is the Jaccard index of title trigrams, 0..1.
func Similarity(a, b string) float64 {
	ta, tb := trigrams(a), trigrams(b)
	if len(ta) == 0 || len(tb) == 0 {
		return 0
	}
	inter := 0
	for k := range ta {
		if tb[k] {
			inter++
		}
	}
	return float64(inter) / float64(len(ta)+len(tb)-inter)
}

// SimilarThreshold: above it the bot asks «Это то же?».
const SimilarThreshold = 0.28
