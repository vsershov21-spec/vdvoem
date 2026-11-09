package app

import "testing"

func TestUrgentText(t *testing.T) {
	for in, want := range map[string]string{
		"Срочно хочу на Мастера и Маргариту, билеты быстро кончаются": "Мастера и Маргариту",
		"срочно: каток в Парке Горького":                              "Каток в Парке Горького",
		"Щелкунчик — билеты разбирают":                                "Щелкунчик",
		"Ужин в грузинском ресторане":                                 "",
		"хочу на джаз": "",
	} {
		got, ok := UrgentText(in)
		if want == "" && ok || want != "" && got != want {
			t.Errorf("%q → %q %v, want %q", in, got, ok, want)
		}
	}
}
