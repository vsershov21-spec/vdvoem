package assistant

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"time"
)

// dialogCases: a refinement after a request — the point of the conversation must survive it. Written apart from the
// training phrases (gen.py REFINE), so this measures carrying the context, not remembering the examples.
type dialogCase struct {
	Prev, Q, Want string
	NoG           []string // kinds that must be gone (the person changed their mind)
}

var dialogCases = []dialogCase{
	{"хочу в бар в субботу", "а где караоке есть?", `{"i":"find","date":"SAT","g":["бар","караоке"]}`, nil},
	{"хочу в бар в субботу", "нет, давай лучше в кофейню", `{"i":"find","date":"SAT","g":["кофейня"]}`, []string{"бар"}},
	{"хочу в бар в субботу", "и чтобы не дороже трёх тысяч", `{"i":"find","date":"SAT","g":["бар"],"max":3000}`, nil},
	{"куда сходить в пятницу вечером", "а подешевле?", `{"i":"find","date":"FRI","part":"evening","max":3000}`, nil},
	{"куда сходить в пятницу вечером", "а в воскресенье?", `{"i":"find","date":"SUN"}`, nil},
	{"хотим в театр на выходных", "только не драма", `{"i":"find","wknd":true,"cats":["culture"]}`, nil},
	{"хотим в театр на выходных", "или может в кино?", `{"i":"find","wknd":true,"g":["кино"]}`, nil},
	{"найди ресторан на завтра", "чтобы можно было спокойно поговорить", `{"i":"find","date":"TOM","g":["ресторан"],"moods":["chill"]}`, nil},
	{"найди ресторан на завтра", "а с живой музыкой?", `{"i":"find","date":"TOM","g":["ресторан"]}`, nil},
	{"что поделать вечером", "что-нибудь активное", `{"i":"find","part":"evening","moods":["active"]}`, nil},
	{"сходим на каток в субботу", "передумала, лучше баня", `{"i":"find","date":"SAT","g":["баня"]}`, []string{"каток"}},
	{"хочу на выставку на неделе", "покажи все варианты", `{"i":"find","cats":["culture"]}`, nil},
	{"хочу на выставку на неделе", "с импрессионистами есть?", `{"i":"find","g":["живопись"]}`, nil},
	{"куда пойти в субботу днём", "а бесплатное что-нибудь?", `{"i":"find","date":"SAT","part":"day","moods":["free"]}`, nil},
	{"хочу в бар в субботу", "а что по дому на неделе?", `{"i":"chores"}`, nil}, // a new topic is not a refinement
	{"хочу в бар в субботу", "обо мне", `{"i":"taste"}`, nil},
}

// EvalDialogs: the share of refinements understood with the request kept
func EvalDialogs(ctx context.Context, as *Assistant, w io.Writer, now time.Time) float64 {
	ok := 0
	fmt.Fprintf(w, "\n### Диалоги: уточнение после запроса · %d\n", len(dialogCases))
	for _, tc := range dialogCases {
		prev, _ := as.Understand(ctx, tc.Prev, Turn{}, now)
		t0 := time.Now()
		got, src := as.Understand(ctx, tc.Q, Turn{Text: tc.Prev, In: prev}, now)
		var want Intent
		_ = json.Unmarshal([]byte(tc.Want), &want)
		good := matches(got, want, now)
		for _, g := range tc.NoG {
			if contains(got.G, g) {
				good = false
			}
		}
		if good {
			ok++
		}
		b, _ := json.Marshal(got)
		fmt.Fprintf(w, "%s %5.2fs %-10s «%s» → «%s»  %s\n", map[bool]string{true: "✓", false: "✗"}[good], time.Since(t0).Seconds(), src, tc.Prev, tc.Q, b)
	}
	fmt.Fprintf(w, "диалоги: верно %d из %d\n", ok, len(dialogCases))
	return float64(ok) / float64(len(dialogCases))
}
