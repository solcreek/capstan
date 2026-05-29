package capstan

import "testing"

func TestNewKnownProviders(t *testing.T) {
	for _, name := range AllProviders() {
		p, err := New(name, "test-token")
		if err != nil {
			t.Fatalf("New(%q): %v", name, err)
		}
		if p.Name() != name {
			t.Errorf("New(%q).Name() = %q, want %q", name, p.Name(), name)
		}
	}
}

func TestNewUnknownProvider(t *testing.T) {
	_, err := New(ProviderName("does-not-exist"), "token")
	if err == nil {
		t.Fatal("expected error for unknown provider")
	}
}

func TestNewEmptyToken(t *testing.T) {
	// Empty token is allowed at construction; provider methods will surface
	// the auth error from the upstream API. This keeps construction pure
	// and testable without forcing every caller to pre-validate tokens.
	if _, err := New(Hetzner, ""); err != nil {
		t.Errorf("empty token should construct: %v", err)
	}
}
