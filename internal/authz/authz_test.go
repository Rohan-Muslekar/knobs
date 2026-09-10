package authz

import "testing"

func TestAtLeast(t *testing.T) {
	roles := []Role{RoleViewer, RoleEditor, RoleAdmin, RoleOwner}

	cases := []struct {
		r, min Role
		want   bool
	}{
		{RoleViewer, RoleViewer, true},
		{RoleViewer, RoleEditor, false},
		{RoleViewer, RoleAdmin, false},
		{RoleViewer, RoleOwner, false},

		{RoleEditor, RoleViewer, true},
		{RoleEditor, RoleEditor, true},
		{RoleEditor, RoleAdmin, false},
		{RoleEditor, RoleOwner, false},

		{RoleAdmin, RoleViewer, true},
		{RoleAdmin, RoleEditor, true},
		{RoleAdmin, RoleAdmin, true},
		{RoleAdmin, RoleOwner, false},

		{RoleOwner, RoleViewer, true},
		{RoleOwner, RoleEditor, true},
		{RoleOwner, RoleAdmin, true},
		{RoleOwner, RoleOwner, true},
	}
	for _, tc := range cases {
		if got := tc.r.AtLeast(tc.min); got != tc.want {
			t.Errorf("Role(%q).AtLeast(%q) = %v, want %v", tc.r, tc.min, got, tc.want)
		}
	}

	// An unrecognized role is below every min, including the lowest real
	// role — it never satisfies AtLeast, not even against RoleViewer.
	unknown := Role("nobody")
	for _, min := range roles {
		if unknown.AtLeast(min) {
			t.Errorf("Role(%q).AtLeast(%q) = true, want false", unknown, min)
		}
	}
	if Role("").AtLeast(RoleViewer) {
		t.Error(`Role("").AtLeast(RoleViewer) = true, want false`)
	}

	// A recognized role can never be "at least" an unrecognized min either
	// — there's no rank to compare against.
	for _, r := range roles {
		if r.AtLeast(unknown) {
			t.Errorf("Role(%q).AtLeast(%q) = true, want false", r, unknown)
		}
	}
}

func TestValid(t *testing.T) {
	cases := []struct {
		r    Role
		want bool
	}{
		{RoleViewer, true},
		{RoleEditor, true},
		{RoleAdmin, true},
		{RoleOwner, true},
		{Role("nobody"), false},
		{Role(""), false},
		{Role("Owner"), false}, // case-sensitive
	}
	for _, tc := range cases {
		if got := Valid(tc.r); got != tc.want {
			t.Errorf("Valid(%q) = %v, want %v", tc.r, got, tc.want)
		}
	}
}
