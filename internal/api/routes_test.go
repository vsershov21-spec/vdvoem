package api

import (
	"net/http"
	"testing"
)

// Route patterns must not clash (net/http panics at start-up otherwise — the server would not boot).
func TestRoutesRegister(t *testing.T) {
	defer func() {
		if p := recover(); p != nil {
			t.Fatal(p)
		}
	}()
	(&API{}).Routes(http.NewServeMux())
}
