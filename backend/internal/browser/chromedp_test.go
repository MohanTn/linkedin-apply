package browser

import (
	"encoding/json"
	"testing"
)

func TestCookiesContain(t *testing.T) {
	raw, _ := json.Marshal([]storedCookie{
		{Name: "bcookie", Value: "x"},
		{Name: "li_at", Value: "AQEDA..."},
	})
	cases := []struct {
		name   string
		cookie string
		want   bool
	}{
		{"present auth cookie", "li_at", true},
		{"absent cookie", "gdId", false},
		{"empty name", "", false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := cookiesContain(raw, c.cookie); got != c.want {
				t.Fatalf("cookiesContain(%q)=%v, want %v", c.cookie, got, c.want)
			}
		})
	}

	// A cookie present but valueless is not a valid session.
	empty, _ := json.Marshal([]storedCookie{{Name: "li_at", Value: ""}})
	if cookiesContain(empty, "li_at") {
		t.Fatal("valueless li_at should not count as authenticated")
	}
	if cookiesContain(nil, "li_at") {
		t.Fatal("nil cookies should not match")
	}
}

func TestSpecsHaveAuthCookies(t *testing.T) {
	for name, s := range specs {
		if s.authCookie == "" {
			t.Errorf("platform %q missing authCookie", name)
		}
	}
}
