package main

import (
	"encoding/json"
	"os"
	"path/filepath"
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

func TestApplySpec_PriceCentsReplaced_OtherFieldsPreserved(t *testing.T) {
	// statusMap keys are in life-cycle order (not alphabetical) — the
	// applySpec round-trip must preserve that order, otherwise every
	// auto-PR ships a noisy statusMap reshuffle on top of the actual
	// price change.
	src := []byte(`{
  "name": "linode",
  "displayName": "Linode",
  "baseUrl": "https://api.linode.com/v4",
  "defaultImage": "linode/ubuntu24.04",
  "userDataEncoding": "base64",
  "statusMap": {
    "provisioning": "initializing",
    "running": "running",
    "deleting": "deleting"
  },
  "priceCents": {
    "g6-old": 500
  },
  "priceCurrency": "USD"
}
`)
	dir := t.TempDir()
	path := filepath.Join(dir, "linode.json")
	if err := os.WriteFile(path, src, 0644); err != nil {
		t.Fatal(err)
	}

	spec, err := capstan.LoadSpec(src)
	if err != nil {
		t.Fatal(err)
	}
	// Stubbed Plans response: one new SKU, one with no API price (skip).
	plans := []capstan.Plan{
		{ID: "g6-new", APIMonthlyCents: 750},
		{ID: "g6-free", APIMonthlyCents: 0},
	}

	changed, err := applySpec(path, spec, plans)
	if err != nil {
		t.Fatal(err)
	}
	if !changed {
		t.Fatal("expected file to change")
	}

	out, _ := os.ReadFile(path)

	// statusMap order preserved verbatim
	provIdx := strings.Index(string(out), `"provisioning"`)
	runIdx := strings.Index(string(out), `"running"`)
	delIdx := strings.Index(string(out), `"deleting"`)
	if provIdx < 0 || runIdx < 0 || delIdx < 0 || !(provIdx < runIdx && runIdx < delIdx) {
		t.Errorf("statusMap key order not preserved:\n%s", out)
	}

	// priceCents rewritten: old gone, new added, free-tier skipped
	var parsed map[string]json.RawMessage
	if err := json.Unmarshal(out, &parsed); err != nil {
		t.Fatal(err)
	}
	var prices map[string]int
	if err := json.Unmarshal(parsed["priceCents"], &prices); err != nil {
		t.Fatal(err)
	}
	if _, ok := prices["g6-old"]; ok {
		t.Error("g6-old should have been removed (not in API)")
	}
	if prices["g6-new"] != 750 {
		t.Errorf("g6-new = %d, want 750", prices["g6-new"])
	}
	if _, ok := prices["g6-free"]; ok {
		t.Error("g6-free should have been skipped (APIMonthlyCents = 0)")
	}

	// Idempotent: second apply returns changed=false.
	changed2, err := applySpec(path, spec, plans)
	if err != nil {
		t.Fatal(err)
	}
	if changed2 {
		t.Error("second apply should be no-op")
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
