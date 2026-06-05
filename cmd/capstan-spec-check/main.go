// capstan-spec-check — compares the embedded provider spec against the
// live provider catalog API and reports drift, optionally writing the
// updated spec back to disk for a PR.
//
// Catches the kind of staleness that bit us in the wild on 2026-05-29:
// specs/hetzner.json carried `cx22` in priceCents, but Hetzner's API
// reports "server type 104 is deprecated" on Create. The tool surfaces:
//
//   - entries in spec.priceCents that the provider no longer returns
//     (likely deprecated — remove from spec)
//   - entries in the live API that aren't in spec.priceCents (new types
//     — need a spec entry with current pricing)
//
// With --apply, the tool rewrites specs/<provider>.json so priceCents
// reflects the live catalog API (slugs + prices both refreshed). All
// other fields are preserved. The weekly CI runs in this mode and
// opens a PR with the diff for human review.
//
// Exit codes:
//   0 — spec is in sync (or --apply succeeded, possibly with changes)
//   1 — drift detected (read-only mode only)
//   2 — error (auth, network, parsing, write)

package main

import (
	"bytes"
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/solcreek/capstan"
)

// tokenAliases — kept in lockstep with cmd/capstan-bench. If the two diverge
// frequently, extract to cmd/internal/envtok.
var tokenAliases = map[capstan.ProviderName][]string{
	capstan.Hetzner: {
		"HCLOUD_TOKEN",
		"HETZNER_API_TOKEN",
		"HETZNER_TOKEN",
	},
	capstan.DigitalOcean: {
		"DIGITALOCEAN_TOKEN",
		"DIGITALOCEAN_ACCESS_TOKEN",
		"DOCTL_ACCESS_TOKEN",
		"DO_API_KEY",
		"DO_TOKEN",
	},
	capstan.Linode: {
		"LINODE_TOKEN",
		"LINODE_CLI_TOKEN",
	},
	capstan.Vultr: {
		"VULTR_API_KEY",
		"VULTR_TOKEN",
	},
}

func tokenForProvider(p capstan.ProviderName) (token, source string) {
	names := tokenAliases[p]
	for _, name := range names {
		if v := os.Getenv(name); v != "" {
			return v, name
		}
	}
	if len(names) > 0 {
		return "", strings.Join(names, " or ")
	}
	return "", string(p)
}

// providersWithPublicCatalog lists providers whose catalog endpoints
// (the data this tool needs to compare against the spec) are reachable
// without authentication. Confirmed empirically 2026-05-29 against
// api.linode.com/v4/linode/types and api.vultr.com/v2/plans. This lets
// CI run drift checks against these two without needing any secrets.
var providersWithPublicCatalog = map[capstan.ProviderName]bool{
	capstan.Linode: true,
	capstan.Vultr:  true,
}

type deprecatedItem struct {
	Name       string `json:"name"`
	PriceCents int    `json:"priceCents"`
}

type drift struct {
	Provider    string           `json:"provider"`
	OnlyInSpec  []deprecatedItem `json:"onlyInSpec"`
	OnlyInAPI   []string         `json:"onlyInAPI"`
	InSyncCount int              `json:"inSyncCount"`

	// Per-location availability drift (plan@location pairs). Only populated for
	// providers that implement capstan.AvailabilityChecker.
	AvailChecked    bool     `json:"availChecked"`
	AvailOnlyInSpec []string `json:"availOnlyInSpec,omitempty"` // spec says orderable, API no longer does
	AvailOnlyInAPI  []string `json:"availOnlyInAPI,omitempty"`  // newly orderable, missing from spec
}

func (d drift) hasDrift() bool {
	return len(d.OnlyInSpec) > 0 || len(d.OnlyInAPI) > 0 ||
		len(d.AvailOnlyInSpec) > 0 || len(d.AvailOnlyInAPI) > 0
}

func main() {
	var (
		providerFlag = flag.String("provider", "hetzner", "provider: hetzner|digitalocean|linode|vultr")
		jsonOut      = flag.Bool("json", false, "emit JSON to stdout instead of Markdown")
		timeoutSec   = flag.Int("timeout", 30, "API call timeout in seconds")
		apply        = flag.Bool("apply", false, "rewrite specs/<provider>.json from live API instead of reporting drift")
		specsDir     = flag.String("specs-dir", "specs", "directory holding <provider>.json files (used with --apply)")
	)
	flag.Parse()

	pName := capstan.ProviderName(*providerFlag)
	token, source := tokenForProvider(pName)
	if token == "" {
		if providersWithPublicCatalog[pName] {
			fmt.Fprintf(os.Stderr, "capstan-spec-check: %s catalog is public, proceeding without token\n", pName)
		} else {
			fmt.Fprintf(os.Stderr, "capstan-spec-check: no token in env (set %s)\n", source)
			os.Exit(2)
		}
	}

	p, err := capstan.New(pName, token)
	if err != nil {
		fmt.Fprintf(os.Stderr, "capstan-spec-check: %v\n", err)
		os.Exit(2)
	}

	ctx, cancel := context.WithTimeout(context.Background(), time.Duration(*timeoutSec)*time.Second)
	defer cancel()

	plans, err := p.Plans(ctx, "")
	if err != nil {
		fmt.Fprintf(os.Stderr, "capstan-spec-check: Plans: %v\n", err)
		os.Exit(2)
	}

	spec := capstan.Spec(pName)
	if spec == nil {
		fmt.Fprintf(os.Stderr, "capstan-spec-check: no embedded spec for %q\n", pName)
		os.Exit(2)
	}

	// Optional: providers that can report per-location orderability (Hetzner).
	var liveAvail map[string][]string
	if ac, ok := p.(capstan.AvailabilityChecker); ok {
		liveAvail, err = ac.Availability(ctx)
		if err != nil {
			fmt.Fprintf(os.Stderr, "capstan-spec-check: Availability: %v\n", err)
			os.Exit(2)
		}
	}

	d := computeDrift(string(pName), plans, spec, liveAvail)

	if *jsonOut {
		json.NewEncoder(os.Stdout).Encode(d)
	} else {
		printMarkdown(os.Stdout, d)
	}

	if *apply {
		path := filepath.Join(*specsDir, string(pName)+".json")
		changed, err := applySpec(path, spec, plans, liveAvail)
		if err != nil {
			fmt.Fprintf(os.Stderr, "capstan-spec-check: apply: %v\n", err)
			os.Exit(2)
		}
		if changed {
			fmt.Fprintf(os.Stderr, "capstan-spec-check: wrote %s\n", path)
		} else {
			fmt.Fprintf(os.Stderr, "capstan-spec-check: %s already in sync\n", path)
		}
		return
	}

	if d.hasDrift() {
		os.Exit(1)
	}
}

// specWriteShape mirrors ProviderSpec's field order for stable
// top-level layout, but uses json.RawMessage for non-price maps so
// the human-curated key order in those fields survives the round-trip.
// Without this, every --apply run would reshuffle statusMap into
// alphabetical and the PR diff would be unreadable noise unrelated
// to actual price changes.
//
// If ProviderSpec gains new fields they need adding here too —
// silently dropped otherwise. Small tax for diff cleanliness.
type specWriteShape struct {
	Name               string              `json:"name"`
	DisplayName        string              `json:"displayName"`
	BaseURL            string              `json:"baseUrl"`
	DefaultImage       string              `json:"defaultImage"`
	UserDataEncoding   string              `json:"userDataEncoding"`
	StatusMap          json.RawMessage     `json:"statusMap"`
	PriceCents         map[string]int      `json:"priceCents"`
	PriceCurrency      string              `json:"priceCurrency"`
	AvailableLocations map[string][]string `json:"availableLocations,omitempty"`
}

// applySpec rewrites the on-disk spec at path with priceCents rebuilt
// from the live API plans. Returns true if the file actually changed —
// the caller (or CI / PR action) uses that signal to decide whether
// there's a PR worth opening.
func applySpec(path string, spec *capstan.ProviderSpec, plans []capstan.Plan, liveAvail map[string][]string) (bool, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return false, fmt.Errorf("read %s: %w", path, err)
	}

	// Decode into specWriteShape so statusMap stays as the raw bytes
	// from the source file — only priceCents gets replaced.
	var current specWriteShape
	if err := json.Unmarshal(raw, &current); err != nil {
		return false, fmt.Errorf("parse %s: %w", path, err)
	}

	newPrices := make(map[string]int, len(plans))
	skippedMissingPrice := []string{}
	for _, pl := range plans {
		if pl.APIMonthlyCents <= 0 {
			// API didn't report a price (e.g. Vultr's free tier). Skip
			// rather than persist 0 — 0 in spec means "unknown" to
			// downstream consumers, and that's exactly the right value
			// here. We log so the PR body can explain the gap.
			skippedMissingPrice = append(skippedMissingPrice, pl.ID)
			continue
		}
		newPrices[pl.ID] = pl.APIMonthlyCents
	}
	current.PriceCents = newPrices
	// Only refresh availability for providers that report it; otherwise preserve
	// whatever the file already carried (decoded into current above).
	if liveAvail != nil {
		current.AvailableLocations = liveAvail
	}

	out, err := marshalSpec(&current)
	if err != nil {
		return false, fmt.Errorf("marshal %s: %w", path, err)
	}

	if bytes.Equal(bytes.TrimRight(raw, "\n"), bytes.TrimRight(out, "\n")) {
		return false, nil
	}
	if err := os.WriteFile(path, out, 0644); err != nil {
		return false, fmt.Errorf("write %s: %w", path, err)
	}
	if len(skippedMissingPrice) > 0 {
		sort.Strings(skippedMissingPrice)
		fmt.Fprintf(os.Stderr, "capstan-spec-check: skipped %d SKUs without API price: %s\n",
			len(skippedMissingPrice), strings.Join(skippedMissingPrice, ", "))
	}
	return true, nil
}

// marshalSpec serializes a spec with the same shape git already tracks
// (2-space indent, trailing newline) so the diff a reviewer sees is
// purely the priceCents delta, not whitespace churn.
func marshalSpec(s *specWriteShape) ([]byte, error) {
	var buf bytes.Buffer
	enc := json.NewEncoder(&buf)
	enc.SetIndent("", "  ")
	enc.SetEscapeHTML(false)
	if err := enc.Encode(s); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

func computeDrift(provider string, plans []capstan.Plan, spec *capstan.ProviderSpec, liveAvail map[string][]string) drift {
	apiSet := make(map[string]bool, len(plans))
	for _, pl := range plans {
		apiSet[pl.ID] = true
	}

	d := drift{Provider: provider}
	for name, price := range spec.PriceCents {
		if apiSet[name] {
			d.InSyncCount++
		} else {
			d.OnlyInSpec = append(d.OnlyInSpec, deprecatedItem{Name: name, PriceCents: price})
		}
	}
	for name := range apiSet {
		if _, ok := spec.PriceCents[name]; !ok {
			d.OnlyInAPI = append(d.OnlyInAPI, name)
		}
	}
	sort.Slice(d.OnlyInSpec, func(i, j int) bool { return d.OnlyInSpec[i].Name < d.OnlyInSpec[j].Name })
	sort.Strings(d.OnlyInAPI)

	// Per-location availability drift (only when the provider reports it).
	if liveAvail != nil {
		d.AvailChecked = true
		specPairs := availPairs(spec.AvailableLocations)
		apiPairs := availPairs(liveAvail)
		for p := range specPairs {
			if !apiPairs[p] {
				d.AvailOnlyInSpec = append(d.AvailOnlyInSpec, p)
			}
		}
		for p := range apiPairs {
			if !specPairs[p] {
				d.AvailOnlyInAPI = append(d.AvailOnlyInAPI, p)
			}
		}
		sort.Strings(d.AvailOnlyInSpec)
		sort.Strings(d.AvailOnlyInAPI)
	}
	return d
}

// availPairs flattens plan -> locations into a set of "plan@location" keys.
func availPairs(m map[string][]string) map[string]bool {
	out := make(map[string]bool)
	for plan, locs := range m {
		for _, loc := range locs {
			out[plan+"@"+loc] = true
		}
	}
	return out
}

func printMarkdown(w io.Writer, d drift) {
	fmt.Fprintf(w, "# capstan-spec-check — %s\n\n", d.Provider)
	fmt.Fprintf(w, "Generated: %s\n\n", time.Now().UTC().Format(time.RFC3339))

	if !d.hasDrift() {
		fmt.Fprintf(w, "Spec is in sync with the live provider catalog (%d types).\n", d.InSyncCount)
		if d.AvailChecked {
			fmt.Fprintf(w, "Per-location availability also in sync.\n")
		}
		return
	}

	if len(d.OnlyInSpec) > 0 {
		fmt.Fprintf(w, "## Likely deprecated — in spec but no longer in API\n\n")
		fmt.Fprintf(w, "Remove these from `specs/%s.json` priceCents:\n\n", d.Provider)
		for _, item := range d.OnlyInSpec {
			fmt.Fprintf(w, "- `%s` (spec price: %d cents)\n", item.Name, item.PriceCents)
		}
		fmt.Fprintln(w)
	}

	if len(d.OnlyInAPI) > 0 {
		fmt.Fprintf(w, "## New in API — missing from spec\n\n")
		fmt.Fprintf(w, "Add these to `specs/%s.json` priceCents (check current pricing in the provider's docs):\n\n", d.Provider)
		for _, name := range d.OnlyInAPI {
			fmt.Fprintf(w, "- `%s`\n", name)
		}
		fmt.Fprintln(w)
	}

	if len(d.AvailOnlyInSpec) > 0 {
		fmt.Fprintf(w, "## No longer orderable — spec lists it, API does not\n\n")
		fmt.Fprintf(w, "Refresh `availableLocations` in `specs/%s.json` (run with --apply):\n\n", d.Provider)
		for _, p := range d.AvailOnlyInSpec {
			fmt.Fprintf(w, "- `%s`\n", p)
		}
		fmt.Fprintln(w)
	}
	if len(d.AvailOnlyInAPI) > 0 {
		fmt.Fprintf(w, "## Newly orderable — in API, missing from spec\n\n")
		fmt.Fprintf(w, "Add to `availableLocations` in `specs/%s.json` (run with --apply):\n\n", d.Provider)
		for _, p := range d.AvailOnlyInAPI {
			fmt.Fprintf(w, "- `%s`\n", p)
		}
		fmt.Fprintln(w)
	}

	fmt.Fprintf(w, "## Summary\n\n")
	fmt.Fprintf(w, "- In sync: %d\n", d.InSyncCount)
	fmt.Fprintf(w, "- Deprecated (only in spec): %d\n", len(d.OnlyInSpec))
	fmt.Fprintf(w, "- New (only in API): %d\n", len(d.OnlyInAPI))
	if d.AvailChecked {
		fmt.Fprintf(w, "- Availability no-longer-orderable (only in spec): %d\n", len(d.AvailOnlyInSpec))
		fmt.Fprintf(w, "- Availability newly-orderable (only in API): %d\n", len(d.AvailOnlyInAPI))
	}
}
