package main

import (
	"strings"
	"testing"

	"github.com/solcreek/capstan"
)

// clearAllAliases sets every known token env var to "" for the test scope.
// t.Setenv guarantees they're restored at test end.
func clearAllAliases(t *testing.T) {
	t.Helper()
	for _, names := range tokenAliases {
		for _, n := range names {
			t.Setenv(n, "")
		}
	}
}

func TestTokenForProviderPrimaryAlias(t *testing.T) {
	clearAllAliases(t)
	t.Setenv("HCLOUD_TOKEN", "primary-value")

	tok, src := tokenForProvider(capstan.Hetzner)
	if tok != "primary-value" {
		t.Errorf("token = %q, want %q", tok, "primary-value")
	}
	if src != "HCLOUD_TOKEN" {
		t.Errorf("source = %q, want HCLOUD_TOKEN", src)
	}
}

func TestTokenForProviderFallbackAlias(t *testing.T) {
	clearAllAliases(t)
	// Only the secondary alias is set; the primary (HCLOUD_TOKEN) is empty.
	t.Setenv("HETZNER_API_TOKEN", "fallback-value")

	tok, src := tokenForProvider(capstan.Hetzner)
	if tok != "fallback-value" {
		t.Errorf("token = %q, want %q", tok, "fallback-value")
	}
	if src != "HETZNER_API_TOKEN" {
		t.Errorf("source = %q, want HETZNER_API_TOKEN", src)
	}
}

func TestTokenForProviderPriorityOrder(t *testing.T) {
	clearAllAliases(t)
	// Both primary and fallback set; primary wins.
	t.Setenv("HCLOUD_TOKEN", "primary")
	t.Setenv("HETZNER_API_TOKEN", "fallback")

	tok, src := tokenForProvider(capstan.Hetzner)
	if tok != "primary" || src != "HCLOUD_TOKEN" {
		t.Errorf("expected HCLOUD_TOKEN priority, got %q/%q", tok, src)
	}
}

func TestTokenForProviderNoneSet(t *testing.T) {
	clearAllAliases(t)

	tok, src := tokenForProvider(capstan.Hetzner)
	if tok != "" {
		t.Errorf("expected empty token, got %q", tok)
	}
	// Source should list every alias for the error message.
	for _, want := range []string{"HCLOUD_TOKEN", "HETZNER_API_TOKEN", "HETZNER_TOKEN"} {
		if !strings.Contains(src, want) {
			t.Errorf("error source %q missing %q", src, want)
		}
	}
}

func TestTokenForProviderAllProviders(t *testing.T) {
	clearAllAliases(t)
	cases := []struct {
		provider capstan.ProviderName
		envName  string
		want     string
	}{
		{capstan.Hetzner, "HCLOUD_TOKEN", "h-tok"},
		{capstan.DigitalOcean, "DIGITALOCEAN_TOKEN", "do-tok"},
		{capstan.Linode, "LINODE_TOKEN", "l-tok"},
		{capstan.Vultr, "VULTR_API_KEY", "v-tok"},
	}
	for _, tc := range cases {
		t.Run(string(tc.provider), func(t *testing.T) {
			clearAllAliases(t)
			t.Setenv(tc.envName, tc.want)
			tok, src := tokenForProvider(tc.provider)
			if tok != tc.want {
				t.Errorf("%s: token = %q, want %q", tc.provider, tok, tc.want)
			}
			if src != tc.envName {
				t.Errorf("%s: source = %q, want %q", tc.provider, src, tc.envName)
			}
		})
	}
}
