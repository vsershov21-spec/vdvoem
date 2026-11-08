package ai

import "testing"

func TestModelSwitch(t *testing.T) {
	c := &Client{URL: "http://127.0.0.1:11434/v1", Model: "qwen2.5:3b"}
	if c.M() != "qwen2.5:3b" || !c.Enabled() {
		t.Fatal(c.M())
	}
	c.SetModel("vdvoem-intent")
	if c.M() != "vdvoem-intent" {
		t.Fatal(c.M())
	}
	c.SetModel("")
	if c.M() != "qwen2.5:3b" {
		t.Fatal(c.M())
	}
}
