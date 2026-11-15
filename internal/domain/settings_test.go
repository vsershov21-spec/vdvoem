package domain

import "testing"

func TestSettings(t *testing.T) {
	seen := map[string]bool{}
	for _, d := range Settings {
		if seen[d.Key] {
			t.Errorf("duplicate key %s", d.Key)
		}
		seen[d.Key] = true
		if d.Scope != ScopeMe && d.Scope != ScopePair {
			t.Errorf("%s: scope %q", d.Key, d.Scope)
		}
		if !ValidSetting(d.Key, d.Default) {
			t.Errorf("%s: its own default %q is not a valid value", d.Key, d.Default)
		}
		if d.Label == "" {
			t.Errorf("%s: no label", d.Key)
		}
	}
	if ValidSetting("meet.budget", "100500") || ValidSetting("notify.quiet", "yes") || ValidSetting("no.such", "1") {
		t.Error("unknown keys and values are refused")
	}
	me := ResolveSettings(ScopeMe, map[string]string{"notify.scout": "1", "notify.quiet": "maybe", "meet.budget": "3000"})
	if me["notify.scout"] != "1" || me["notify.quiet"] != "1" || me["notify.proposals"] != "1" {
		t.Errorf("stored over defaults, junk ignored: %v", me)
	}
	if _, ok := me["meet.budget"]; ok {
		t.Error("a couple's setting does not leak into mine")
	}
	if p := ResolveSettings(ScopePair, nil); p["chores.keep"] != "7" || p["meet.budget"] != "5000" {
		t.Errorf("pair defaults: %v", p)
	}
}
