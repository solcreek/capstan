// capstan-spec-check — compares the embedded provider spec against the
// live provider catalog API and reports drift.
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
// Exit codes:
//   0 — spec is in sync with API
//   1 — drift detected (output describes what changed)
//   2 — error (auth, network, parsing)
//
// Designed to be CI-friendly: run weekly, file an issue / open a PR with
// the diff when exit != 0.

package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"os"
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
}

func (d drift) hasDrift() bool {
	return len(d.OnlyInSpec) > 0 || len(d.OnlyInAPI) > 0
}

func main() {
	var (
		providerFlag = flag.String("provider", "hetzner", "provider: hetzner|digitalocean|linode|vultr")
		jsonOut      = flag.Bool("json", false, "emit JSON to stdout instead of Markdown")
		timeoutSec   = flag.Int("timeout", 30, "API call timeout in seconds")
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

	d := computeDrift(string(pName), plans, spec)

	if *jsonOut {
		json.NewEncoder(os.Stdout).Encode(d)
	} else {
		printMarkdown(os.Stdout, d)
	}

	if d.hasDrift() {
		os.Exit(1)
	}
}

func computeDrift(provider string, plans []capstan.Plan, spec *capstan.ProviderSpec) drift {
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
	return d
}

func printMarkdown(w io.Writer, d drift) {
	fmt.Fprintf(w, "# capstan-spec-check — %s\n\n", d.Provider)
	fmt.Fprintf(w, "Generated: %s\n\n", time.Now().UTC().Format(time.RFC3339))

	if !d.hasDrift() {
		fmt.Fprintf(w, "Spec is in sync with the live provider catalog (%d types).\n", d.InSyncCount)
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

	fmt.Fprintf(w, "## Summary\n\n")
	fmt.Fprintf(w, "- In sync: %d\n", d.InSyncCount)
	fmt.Fprintf(w, "- Deprecated (only in spec): %d\n", len(d.OnlyInSpec))
	fmt.Fprintf(w, "- New (only in API): %d\n", len(d.OnlyInAPI))
}
