---
name: solcreek-capstan-overview
description: |
  Multi-provider VPS catalog and placement lookup CLI for Hetzner /
  DigitalOcean / Linode / Vultr. Use when you need to list or compare
  server plans across providers, look up monthly prices, get a
  placement recommendation (geography + workload + SLA → provider /
  region / size), or check spec drift against live provider APIs.
  JSON-by-default output, stable error codes, no shell prompts. The
  read commands are offline (embedded specs) and instant — no token
  required.
license: MIT
metadata:
  author: solcreek
  version: '0.5.0'
  bin: capstan
  npm: capstan
  homepage: https://github.com/solcreek/capstan
---

# Capstan — Multi-Provider VPS CLI

`capstan` is the one-binary lookup surface for VPS provider catalogs.
JSON output is the default — `--text` switches to human view. Every
error is JSON on stderr with a stable `code` so you can branch without
parsing English.

## When to call this

- "Which providers does capstan support?" → `capstan providers`
- "What plans does Hetzner offer right now?" → `capstan plans hetzner`
- "What does cx23 cost on Hetzner?" → `capstan price hetzner cx23`
- "Best provider / region / size for my workload in Japan?" → `capstan recommend --geo japan --workload cpu-latency`
- "What does the `plans` command actually return?" → `capstan describe plans`
- "Is the bundled spec still aligned with the provider?" → `npx capstan-spec-check --provider hetzner` (companion Go tool)

If a CLI call is more than one or two short args, you probably want
`capstan describe <command>` first to see the exact schema.

## Commands at a glance

```
capstan providers                          List 4 provider names
capstan workloads                          io-multitenant | cpu-latency | cpu-throughput | general
capstan sla-tiers                          best-effort | standard | premium
capstan geographies                        14 geographies (eu-central, japan, latam, …)
capstan plans <provider>                   Plans sorted by monthly cents
capstan price <provider> <plan>            Single plan price lookup
capstan recommend --geo <g> [--workload]   Placement recommendation
                  [--sla]
capstan describe [command]                 Runtime schema for any command
capstan skill [name]                       Emit a bundled skill (this file)
```

## Output shape

Every command emits:

```json
{ "ok": true, "<command-specific-fields>": ... }
```

Errors come on stderr:

```json
{ "ok": false, "error": "<message>", "code": "<stable-code>" }
```

Stable error codes:

| Code | Meaning | Likely cause |
|---|---|---|
| `unknown_subcommand` | The subcommand name isn't recognized | Typo, or running an old capstan version |
| `unknown_provider` | Provider name not one of the 4 supported | Typo; valid: `hetzner`, `digitalocean`, `linode`, `vultr` |
| `unknown_plan` | Plan id not in the spec for that provider | **Spec drift** — re-run `capstan-spec-check` or pick from `capstan plans <provider>` |
| `missing_arg` | A required positional or flag is absent | Read `capstan describe <command>` for the schema |
| `bad_arg` | The arg is present but rejected (control chars, query injection, unknown enum) | Strip control chars; pass bare slugs only; pick from the enum |
| `recommend_failed` | Posture had no placement for the requested combination | Try a broader workload class or SLA tier |

Exit codes: `0` success, `1` logic error (unknown plan, drift, etc.),
`2` user error (bad args, missing token, unknown subcommand).

## Important invariants

These are not obvious from `--help`. They are the things you'd
otherwise have to learn the hard way.

1. **`plans` and `price` are read from the embedded spec, not the
   live API.** That makes them sub-millisecond but means the data is
   only as fresh as the last spec sync. A weekly `spec-drift` CI job
   alerts when a provider's catalog moves; if `unknown_plan` fires on
   a plan you believe should exist, suspect drift first
2. **The CLI rejects identifiers with query / fragment / encoding /
   control characters.** This is deliberate — `cx23?fields=name`,
   `cx23\x00`, `%2e%2e` all return `bad_arg`. Pass bare slugs only
3. **`recommend` returns `primary` plus `fallbacks` plus `caveats`.**
   The fallbacks are a retry ladder; iterate them in order if the
   primary placement fails with a transient provider error. The
   caveats are real and worth surfacing to the human caller
4. **Token aliases match every common vendor convention.** For live
   commands (when they ship): `HCLOUD_TOKEN` | `HETZNER_API_TOKEN` |
   `HETZNER_TOKEN` for Hetzner; the DO list is `DIGITALOCEAN_TOKEN` |
   `DIGITALOCEAN_ACCESS_TOKEN` | `DOCTL_ACCESS_TOKEN` | `DO_API_KEY` |
   `DO_TOKEN`. Same pattern for Linode / Vultr
5. **Linode and Vultr catalog endpoints are public.** Drift checks
   against those two require no token. Hetzner and DigitalOcean need
   read-only tokens

## Known caveats

These come from real bench runs on 2026-05-29 and inform when to
suggest fallbacks to the user:

- **Hetzner `cax11`/`fsn1` placement frequently fails** with HTTP 412
  `resource_unavailable` at peak hours (ARM capacity at Falkenstein
  is tight). Prefer `cx23` or fall back to `cax11`/`nbg1`
- **Hetzner CCX dedicated tier raised prices significantly in 2026**
  (ccx53 +70%; ccx43 +60%). The spec is current, but if you cached
  price data before May 2026, it's wrong
- **Hetzner deprecated cx22 / cx32 / cx42 / cx52.** Create returns
  `422 server type N is deprecated`. The spec no longer lists them
- **Hetzner `cpx12` exists only at `sin` (Singapore).** It does not
  appear in fsn1 / nbg1 / hel1 / ash / hil

## Don't

- Don't append query strings to plan IDs (`cx23?...`) — `bad_arg`
- Don't pre-URL-encode arguments (`%2e%2e`) — `bad_arg`
- Don't pass the plan id from one provider to another (`s-1vcpu-1gb`
  is DigitalOcean, not Hetzner)
- Don't cache `capstan plans` output across long sessions — the spec
  evolves; query fresh
- Don't assume the absolute prices stay constant — providers update;
  trust what the spec says today

## Companion Go tools

The repo ships two Go binaries that complement this CLI:

- `capstan-bench` — measures provider API latency end-to-end (reads,
  fanout, full mutation cycle). Useful for IPC architecture decisions
  ("can the caller use subprocess per call, or does it need a
  long-lived sidecar?")
- `capstan-spec-check` — compares the embedded spec against the live
  catalog API and reports drift. Linode and Vultr endpoints public,
  Hetzner and DigitalOcean need tokens

Both are agent-friendly: structured output, stable exit codes,
documented error classes.
