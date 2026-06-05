package capstan

import "testing"

func TestIsAvailable(t *testing.T) {
	// No availability data at all => permissive (never block an unmapped provider).
	if !(&ProviderSpec{}).IsAvailable("cpx11", "nbg1") {
		t.Fatal("empty AvailableLocations should be permissive")
	}

	s := &ProviderSpec{AvailableLocations: map[string][]string{
		"cpx11": {"ash", "hil"},
		"cx23":  {"fsn1", "nbg1"},
	}}
	cases := []struct {
		plan, loc string
		want      bool
	}{
		{"cpx11", "ash", true},
		{"cpx11", "nbg1", false}, // the real bug: cpx11 is priced in nbg1 but not orderable there
		{"cpx11", "sin", false},
		{"cx23", "nbg1", true},
		{"cx23", "sin", false},
		{"brand-new-type", "nbg1", true}, // unknown plan => don't block, let the API decide
	}
	for _, c := range cases {
		if got := s.IsAvailable(c.plan, c.loc); got != c.want {
			t.Errorf("IsAvailable(%q,%q)=%v, want %v", c.plan, c.loc, got, c.want)
		}
	}
	if got := s.AvailableLocationsFor("cpx11"); len(got) != 2 {
		t.Errorf("AvailableLocationsFor(cpx11)=%v, want 2 locations", got)
	}
	if got := s.AvailableLocationsFor("nope"); got != nil {
		t.Errorf("AvailableLocationsFor(unknown)=%v, want nil", got)
	}
}
