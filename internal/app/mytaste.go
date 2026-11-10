package app

import (
	"context"
	"encoding/json"
	"fmt"
	"sort"
	"strings"

	"vdvoem/internal/leisure"
	"vdvoem/internal/store"
)

// «Мой вкус»: what a person says about themselves, in three levels — spheres (театр, музыка…), the kinds inside
// them (джаз, баня, иммерсивный…), and how (когда, бюджет, темп, формат, настроение). It is the start the marks
// then refine: the ranking leans on it at once, the scout reads it, the assistant tells it back as a portrait.
// Weights: 2 — очень, 1 — нравится, -1 — не моё.
type MyTaste struct {
	Spheres map[string]int `json:"spheres"`
	Tags    map[string]int `json:"tags"`
	When    []string       `json:"when"`   // weekday_evening weekend_day weekend_evening morning
	Budget  string         `json:"budget"` // 2000 5000 10000 any
	Pace    string         `json:"pace"`   // calm active mix
	Style   []string       `json:"style"`  // classic modern experimental
	Scale   string         `json:"scale"`  // intimate big any
	Moods   []string       `json:"moods"`  // romantic fun new adrenaline chill
	Note    string         `json:"note"`   // in their own words
	At      int64          `json:"at"`     // last change, unix
}

var tasteFacets = map[string][]string{
	"when":  {"weekday_evening", "weekend_day", "weekend_evening", "morning"},
	"style": {"classic", "modern", "experimental"},
	"moods": {"romantic", "fun", "new", "adrenaline", "chill"},
}

func (t MyTaste) Empty() bool { return len(t.Spheres) == 0 && len(t.Tags) == 0 }

func (a *App) Taste(ctx context.Context, memberID int64) MyTaste {
	var t MyTaste
	_ = json.Unmarshal([]byte(a.S.Setting(ctx, fmt.Sprintf("taste:%d", memberID))), &t)
	if t.Spheres == nil {
		t.Spheres = map[string]int{}
	}
	if t.Tags == nil {
		t.Tags = map[string]int{}
	}
	return t
}

func clampW(w int) int {
	switch {
	case w >= 2:
		return 2
	case w <= -1:
		return -1
	}
	return w
}

func only(l []string, allowed []string, max int) []string {
	ok := map[string]bool{}
	for _, x := range allowed {
		ok[x] = true
	}
	var out []string
	seen := map[string]bool{}
	for _, x := range l {
		if ok[x] && !seen[x] && len(out) < max {
			out, seen[x] = append(out, x), true
		}
	}
	return out
}

// SetTaste saves the whole profile as the person set it (unknown spheres, kinds and facets are dropped).
func (a *App) SetTaste(ctx context.Context, c Couple, t MyTaste) (MyTaste, error) {
	clean := MyTaste{Spheres: map[string]int{}, Tags: map[string]int{}}
	for id, w := range t.Spheres {
		for _, s := range leisure.Spheres {
			if s.ID == id && clampW(w) != 0 {
				clean.Spheres[id] = clampW(w)
			}
		}
	}
	for tag, w := range t.Tags {
		if leisure.Known(tag) && clampW(w) != 0 {
			clean.Tags[tag] = clampW(w)
		}
	}
	clean.When = only(t.When, tasteFacets["when"], 4)
	clean.Style = only(t.Style, tasteFacets["style"], 3)
	clean.Moods = only(t.Moods, tasteFacets["moods"], 5)
	if x := only([]string{t.Budget}, []string{"2000", "5000", "10000", "any"}, 1); len(x) > 0 {
		clean.Budget = x[0]
	}
	if x := only([]string{t.Pace}, []string{"calm", "active", "mix"}, 1); len(x) > 0 {
		clean.Pace = x[0]
	}
	if x := only([]string{t.Scale}, []string{"intimate", "big", "any"}, 1); len(x) > 0 {
		clean.Scale = x[0]
	}
	if r := []rune(strings.TrimSpace(t.Note)); len(r) > 300 {
		clean.Note = string(r[:300])
	} else {
		clean.Note = string(r)
	}
	clean.At = a.now().Unix()
	b, _ := json.Marshal(clean)
	if err := a.S.SetSetting(ctx, fmt.Sprintf("taste:%d", c.Me.ID), string(b)); err != nil {
		return clean, err
	}
	a.Live.From(ctx, c.Space.ID)
	return clean, nil
}

// NudgeTaste: a word in passing («обожаю джаз», «не люблю стендап») moves the profile, never resets it.
func (a *App) NudgeTaste(ctx context.Context, c Couple, text string) []string {
	tags := leisure.Detect(strings.NewReplacer("не люблю", "", "терпеть не могу", "", "не моё", "", "не мое", "").Replace(text))
	if len(tags) == 0 {
		return nil
	}
	low := strings.ToLower(text)
	w := 1
	switch {
	case strings.Contains(low, "не люб") || strings.Contains(low, "терпеть не") || strings.Contains(low, "не моё") || strings.Contains(low, "не мое") || strings.Contains(low, "ненавиж") || strings.Contains(low, "не хочу больше"):
		w = -1
	case strings.Contains(low, "обожа") || strings.Contains(low, "очень люб") || strings.Contains(low, "фанат"):
		w = 2
	}
	t := a.Taste(ctx, c.Me.ID)
	for _, tag := range tags {
		t.Tags[tag] = w
		if s := leisure.SphereOf(tag); s != "" && w > 0 && t.Spheres[s] <= 0 {
			t.Spheres[s] = 1
		}
	}
	_, _ = a.SetTaste(ctx, c, t)
	return tags
}

// tasteBias: how much the person's own words move a forecast for this idea (−0.4…+0.3)
func tasteBias(t MyTaste, item []string, category string) float64 {
	if t.Empty() {
		return 0
	}
	d := 0.0
	hit := false
	for _, g := range item {
		if w, ok := t.Tags[g]; ok {
			hit = true
			if w < 0 {
				d -= 0.35
			} else {
				d += 0.12 * float64(w)
			}
			continue
		}
		if w, ok := t.Spheres[leisure.SphereOf(g)]; ok {
			hit = true
			if w < 0 {
				d -= 0.15
			} else {
				d += 0.06 * float64(w)
			}
		}
	}
	if !hit { // nothing named: the category's sphere, lightly
		s := map[string]string{"food": "food", "walk": "walk", "sport": "active"}[category]
		if w, ok := t.Spheres[s]; ok {
			d += map[bool]float64{true: -0.1, false: 0.04 * float64(w)}[w < 0]
		}
	}
	if d > 0.3 {
		d = 0.3
	}
	if d < -0.4 {
		d = -0.4
	}
	return d
}

// TasteCloud: what the app knows about a person — said by them (strong) and noticed from marks (light).
type CloudTag struct {
	Tag    string `json:"tag"`
	W      int    `json:"w"`      // 2 очень, 1 нравится, -1 не моё
	Said   bool   `json:"said"`   // they said it; false — noticed from the marks
	Sphere string `json:"sphere"` // for the colour
}

func (a *App) TasteCloud(ctx context.Context, c Couple, memberID int64) []CloudTag {
	t := a.Taste(ctx, memberID)
	var out []CloudTag
	seen := map[string]bool{}
	for tag, w := range t.Tags {
		out = append(out, CloudTag{tag, w, true, leisure.SphereOf(tag)})
		seen[tag] = true
	}
	for _, s := range leisure.Spheres {
		if w, ok := t.Spheres[s.ID]; ok {
			// a sphere is in the cloud only when none of its kinds is: «театр» or «драма, комедия», not both
			named := false
			for _, tag := range s.Tags {
				named = named || seen[tag]
			}
			if !named {
				out = append(out, CloudTag{strings.ToLower(s.Title), w, true, s.ID})
				seen[strings.ToLower(s.Title)] = true
			}
		}
	}
	// noticed: the taste model's strongest words, when they are kinds the person did not name
	ts := a.Tastes(ctx, c)
	m := ts.Me
	if c.Partner != nil && memberID == c.Partner.ID {
		m = ts.Partner
	}
	likes, dislikes := m.Explain(8)
	for _, l := range likes {
		if leisure.Known(l) && !seen[l] {
			out, seen[l] = append(out, CloudTag{l, 1, false, leisure.SphereOf(l)}), true
		}
	}
	for _, l := range dislikes {
		if leisure.Known(l) && !seen[l] {
			out, seen[l] = append(out, CloudTag{l, -1, false, leisure.SphereOf(l)}), true
		}
	}
	sort.SliceStable(out, func(i, j int) bool {
		if out[i].W != out[j].W {
			return out[i].W > out[j].W
		}
		if out[i].Said != out[j].Said {
			return out[i].Said
		}
		return out[i].Tag < out[j].Tag
	})
	return out
}

var facetWords = map[string]string{
	"weekday_evening": "вечера будней", "weekend_day": "дни выходных", "weekend_evening": "вечера выходных", "morning": "утро",
	"classic": "классика", "modern": "современное", "experimental": "эксперименты",
	"romantic": "романтика", "fun": "посмеяться", "new": "новое", "adrenaline": "адреналин", "chill": "спокойно отдохнуть",
	"calm": "спокойный темп", "active": "активный темп", "mix": "по-разному",
	"intimate": "камерно", "big": "масштабно",
}

// Portrait: the person in a few lines, from their own words first — for the assistant and the scout
func (a *App) Portrait(ctx context.Context, c Couple, memberID int64) string {
	t := a.Taste(ctx, memberID)
	cloud := a.TasteCloud(ctx, c, memberID)
	var love, like, no, noticed []string
	for _, x := range cloud {
		switch {
		case !x.Said && x.W > 0:
			noticed = append(noticed, x.Tag)
		case x.W == 2:
			love = append(love, x.Tag)
		case x.W == 1:
			like = append(like, x.Tag)
		case x.W < 0:
			no = append(no, x.Tag)
		}
	}
	var b []string
	if len(love) > 0 {
		b = append(b, "Очень: "+strings.Join(love, ", "))
	}
	if len(like) > 0 {
		b = append(b, "Нравится: "+strings.Join(like, ", "))
	}
	if len(no) > 0 {
		b = append(b, "Не твоё: "+strings.Join(no, ", "))
	}
	var how []string
	for _, x := range append(append(append([]string{}, t.When...), t.Style...), t.Moods...) {
		how = append(how, facetWords[x])
	}
	if w := facetWords[t.Pace]; w != "" {
		how = append(how, w)
	}
	if w := facetWords[t.Scale]; w != "" {
		how = append(how, w)
	}
	if t.Budget != "" && t.Budget != "any" {
		how = append(how, "до "+t.Budget+" ₽ на двоих")
	}
	if len(how) > 0 {
		b = append(b, "Как: "+strings.Join(how, ", "))
	}
	if len(noticed) > 0 {
		b = append(b, "Заметил по отметкам: "+strings.Join(noticed, ", "))
	}
	return strings.Join(b, "\n")
}

// itemKinds: the kinds an idea is, as the ranking sees it
func itemKinds(i store.Idea) []string {
	var out []string
	for _, g := range itemOf(i).Genres {
		if leisure.Known(g) {
			out = append(out, g)
		}
	}
	return out
}
