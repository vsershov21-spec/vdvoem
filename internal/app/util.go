package app

import (
	"encoding/json"
	"net/url"
)

func jsonUnmarshal(raw []byte, v any) error { return json.Unmarshal(raw, v) }

func urlq(s string) string { return url.QueryEscape(s) }
