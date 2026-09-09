package api

import "testing"

// TestParseAuditLimit exercises the default/floor/cap clamp logic directly,
// with no DB involved. It lives in package api (not api_test) because
// parseAuditLimit is unexported.
func TestParseAuditLimit(t *testing.T) {
	cases := []struct {
		name string
		raw  string
		want int
	}{
		{"absent", "", 100},
		{"unparseable", "abc", 100},
		{"zero floors to minimum", "0", 1},
		{"negative floors to minimum", "-5", 1},
		{"above max caps at maximum", "999", 500},
		{"within range passes through", "50", 50},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := parseAuditLimit(tc.raw); got != tc.want {
				t.Fatalf("parseAuditLimit(%q) = %d, want %d", tc.raw, got, tc.want)
			}
		})
	}
}
