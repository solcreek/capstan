package main

import (
	"strings"
	"testing"

	"github.com/solcreek/capstan"
)

func TestComputeDriftAllInSync(t *testing.T) {
	plans := []capstan.Plan{
		{ID: "cx23"}, {ID: "cx33"}, {ID: "cax11"},
	}
	spec := &capstan.ProviderSpec{
		PriceCents: map[string]int{
			"cx23": 499, "cx33": 799, "cax11": 549,
		},
	}
	d := computeDrift("hetzner", plans, spec)
	if d.hasDrift() {
		t.Errorf("expected no drift, got OnlyInSpec=%v OnlyInAPI=%v", d.OnlyInSpec, d.OnlyInAPI)
	}
	if d.InSyncCount != 3 {
		t.Errorf("InSyncCount = %d, want 3", d.InSyncCount)
	}
}

func TestComputeDriftDeprecatedInSpec(t *testing.T) {
	// Reproduces the 2026-05-29 cx22 case.
	plans := []capstan.Plan{
		{ID: "cx23"}, {ID: "cx33"},
	}
	spec := &capstan.ProviderSpec{
		PriceCents: map[string]int{
			"cx22": 449, "cx23": 499, "cx33": 799,
		},
	}
	d := computeDrift("hetzner", plans, spec)
	if !d.hasDrift() {
		t.Fatal("expected drift, got none")
	}
	if len(d.OnlyInSpec) != 1 {
		t.Fatalf("OnlyInSpec = %v, want 1 entry", d.OnlyInSpec)
	}
	if d.OnlyInSpec[0].Name != "cx22" {
		t.Errorf("OnlyInSpec[0].Name = %q, want cx22", d.OnlyInSpec[0].Name)
	}
	if d.OnlyInSpec[0].PriceCents != 449 {
		t.Errorf("OnlyInSpec[0].PriceCents = %d, want 449", d.OnlyInSpec[0].PriceCents)
	}
	if d.InSyncCount != 2 {
		t.Errorf("InSyncCount = %d, want 2", d.InSyncCount)
	}
}

func TestComputeDriftNewInAPI(t *testing.T) {
	plans := []capstan.Plan{
		{ID: "cx23"}, {ID: "cx33"}, {ID: "cx63"}, // cx63 hypothetical new type
	}
	spec := &capstan.ProviderSpec{
		PriceCents: map[string]int{"cx23": 499, "cx33": 799},
	}
	d := computeDrift("hetzner", plans, spec)
	if !d.hasDrift() {
		t.Fatal("expected drift")
	}
	if len(d.OnlyInAPI) != 1 || d.OnlyInAPI[0] != "cx63" {
		t.Errorf("OnlyInAPI = %v, want [cx63]", d.OnlyInAPI)
	}
}

func TestComputeDriftBothSides(t *testing.T) {
	plans := []capstan.Plan{
		{ID: "cx23"}, {ID: "cx63"}, // 23 in sync, 63 new
	}
	spec := &capstan.ProviderSpec{
		PriceCents: map[string]int{
			"cx22": 449, // deprecated
			"cx23": 499, // in sync
		},
	}
	d := computeDrift("hetzner", plans, spec)
	if len(d.OnlyInSpec) != 1 || d.OnlyInSpec[0].Name != "cx22" {
		t.Errorf("OnlyInSpec mismatch: %v", d.OnlyInSpec)
	}
	if len(d.OnlyInAPI) != 1 || d.OnlyInAPI[0] != "cx63" {
		t.Errorf("OnlyInAPI mismatch: %v", d.OnlyInAPI)
	}
	if d.InSyncCount != 1 {
		t.Errorf("InSyncCount = %d, want 1", d.InSyncCount)
	}
}

func TestTokenForProviderFallback(t *testing.T) {
	for _, names := range tokenAliases {
		for _, n := range names {
			t.Setenv(n, "")
		}
	}
	t.Setenv("HETZNER_API_TOKEN", "tok")
	tok, src := tokenForProvider(capstan.Hetzner)
	if tok != "tok" || src != "HETZNER_API_TOKEN" {
		t.Errorf("got %q/%q", tok, src)
	}

	// None set
	t.Setenv("HETZNER_API_TOKEN", "")
	tok, src = tokenForProvider(capstan.Hetzner)
	if tok != "" {
		t.Errorf("expected empty token")
	}
	if !strings.Contains(src, "HCLOUD_TOKEN") {
		t.Errorf("source missing alias list: %q", src)
	}
}
