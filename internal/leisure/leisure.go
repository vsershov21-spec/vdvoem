// Package leisure is the map of everything a couple might go to: ~90 kinds of leisure (genres of theatre and
// music, exhibitions, walks, food, games, sport, wellness, events…), each with the words people use for it.
// One list, used everywhere: the rules read a request with it, the assistant searches with it, ideas get
// tagged with it, and the training set for the model is generated from it (deploy/train/gen.py reads kinds.json).
package leisure

import (
	_ "embed"
	"encoding/json"
	"sort"
	"strings"
	"unicode"
)

//go:embed kinds.json
var raw []byte

type Kind struct {
	Tag   string   `json:"tag"`
	Cat   string   `json:"cat"`
	Say   []string `json:"say"`
	Stems []string `json:"stems"`
}

var Kinds []Kind
var byTag = map[string]Kind{}
var narrower map[string][]string // a broad kind gives way to a narrower one named with it

func init() {
	var d struct {
		Kinds    []Kind              `json:"kinds"`
		Narrower map[string][]string `json:"narrower"`
	}
	if err := json.Unmarshal(raw, &d); err != nil {
		panic("leisure: " + err.Error())
	}
	Kinds, narrower = d.Kinds, d.Narrower
	for _, k := range Kinds {
		byTag[k.Tag] = k
	}
}

func norm(s string) string { // lower case, ё→е, punctuation → spaces (a hyphen stays: «инди-рок»)
	s = strings.ReplaceAll(strings.ToLower(s), "ё", "е")
	s = strings.Map(func(r rune) rune {
		if unicode.IsLetter(r) || unicode.IsDigit(r) || r == '-' || r == '+' {
			return r
		}
		return ' '
	}, s)
	return " " + s + " "
}

// at: the stem starts a word somewhere in t (so «рок» is not found in «срок», «бар» not in «барабан» with a space)
// A stem of several words matches word by word, each a prefix («комнат страх» finds «комнату страха»).
func at(t, stem string) bool {
	stem = strings.ReplaceAll(strings.ToLower(stem), "ё", "е")
	if w := strings.Fields(stem); len(w) > 1 && !strings.HasSuffix(stem, " ") {
		return atWords(strings.Fields(t), w)
	}
	for i := 0; ; {
		k := strings.Index(t[i:], stem)
		if k < 0 {
			return false
		}
		k += i
		if k == 0 || !unicode.IsLetter(lastRune(t[:k])) {
			return true
		}
		i = k + 1
		if i >= len(t) {
			return false
		}
	}
}

// wanted: the text without what is turned down — «без стендапа», «кроме квестов», «не хотим караоке» —
// and without the context of an evening already planned («после театра», «перед кино»)
func wanted(t string) string {
	w := strings.Fields(t)
	no := map[string]bool{"без": true, "кроме": true, "не": true, "никаких": true, "никакого": true, "никакой": true, "нет": true}
	verb := map[string]bool{"хочу": true, "хотим": true, "люблю": true, "любим": true, "надо": true, "нужно": true, "нужен": true, "нужна": true, "предлагай": true}
	out := make([]string, 0, len(w))
	ctx := 0 // «где поесть после планетария»: what comes after «после» is the plan already made, not the ask
	for i, x := range w {
		if x == "после" || x == "перед" {
			ctx = 3
			continue
		}
		if ctx > 0 {
			ctx--
			continue
		}
		if i > 0 && no[w[i-1]] && !no[x] || i > 1 && w[i-2] == "не" && verb[w[i-1]] {
			continue
		}
		out = append(out, x)
	}
	return " " + strings.Join(out, " ") + " "
}

func atWords(t, w []string) bool {
	for i := 0; i+len(w) <= len(t); i++ {
		ok := true
		for j, x := range w {
			if !strings.HasPrefix(t[i+j], x) {
				ok = false
				break
			}
		}
		if ok {
			return true
		}
	}
	return false
}

func lastRune(s string) rune {
	r := []rune(s)
	if len(r) == 0 {
		return ' '
	}
	return r[len(r)-1]
}

// Detect: the kinds a text speaks of, most specific first (an «органный концерт при свечах» is both)
func Detect(text string) []string {
	t := wanted(norm(text))
	var out []string
	for _, k := range Kinds {
		for _, s := range k.Stems {
			if at(t, s) {
				out = append(out, k.Tag)
				break
			}
		}
	}
	// a broad kind is dropped when a narrower one of the same family says more («джаз» over «концерт»)
	drop := map[string]bool{}
	has := func(tag string) bool {
		for _, x := range out {
			if x == tag {
				return true
			}
		}
		return false
	}
	for broad, narrow := range narrower {
		for _, n := range narrow {
			if has(n) {
				drop[broad] = true
			}
		}
	}
	res := out[:0]
	for _, x := range out {
		if !drop[x] {
			res = append(res, x)
		}
	}
	return res
}

// Cat: the category a kind belongs to ("" — unknown)
func Cat(tag string) string { return byTag[tag].Cat }

// Known: whether a tag is on the map
func Known(tag string) bool { _, ok := byTag[tag]; return ok }

// Tags: every tag, sorted (the model's vocabulary)
func Tags() []string {
	out := make([]string, 0, len(Kinds))
	for _, k := range Kinds {
		out = append(out, k.Tag)
	}
	sort.Strings(out)
	return out
}

// Fits: whether an event's own text (title, description, genres) is of this kind
func Fits(tag, text string) bool {
	k, ok := byTag[tag]
	if !ok {
		return false
	}
	t := norm(text)
	if at(t, tag) {
		return true
	}
	for _, s := range k.Stems {
		if at(t, s) {
			return true
		}
	}
	return false
}

// Narrow drops a broad kind when a narrower one of its family is in the list too (for kinds gathered from
// several readers: the model's and the words')
func Narrow(tags []string) []string {
	in := map[string]bool{}
	for _, x := range tags {
		in[x] = true
	}
	var out []string
	for _, x := range tags {
		keep := true
		for _, n := range narrower[x] {
			if in[n] {
				keep = false
				break
			}
		}
		if keep {
			out = append(out, x)
		}
	}
	return out
}
