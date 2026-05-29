# Changelog

## v0.5.0 — Server List + power actions, factory, bench + spec-check tools, Hetzner spec sync (Go)

The v0.5 cycle is Go-only. The TypeScript `Provider` interface stays at
the v0.4 surface — TS catches up when a TS consumer needs the new
operations; speculative parity is explicitly out of scope. JSON specs
in `specs/` remain the cross-language source of truth.

### Provider interface — read + power lifecycle

```go
type Provider interface {
    // existing: Name, Regions, Plans, Create, Get, Destroy, EstimateMonthlyCost
    List(ctx, ListOpts) ([]Server, error)
    PowerOn(ctx, id) (*Action, error)
    PowerOff(ctx, id) (*Action, error)
    Restart(ctx, id) (*Action, error)
    WaitForAction(ctx, actionID) (*Action, error)
}
```

`List` auto-paginates by default (`ListOpts.MaxServers` caps the
result, default 200; `ListOpts.Label` filters by provider tag).
Power actions return an `Action` immediately with the underlying
async work tracked by ID; `WaitForAction` polls `/actions/{id}` on a
500ms tick until terminal status, deadline via `ctx`.

**Hetzner** is the reference implementation. DigitalOcean / Linode /
Vultr expose the same five methods but return `capstan.ErrNotImplemented`
for now; the interface is unified so consumers write provider-agnostic
code today and surface "not yet supported" at the edge.

### Provider construction — `New(name, token)` factory

Mirrors the TypeScript `createProvider` helper:

```go
p, err := capstan.New(capstan.Hetzner, token)
// instead of: p := capstan.NewHetzner(token), with a switch per name
```

### HTTP plumbing — single shared `httpClient`

Each provider previously carried its own `(get, post, del)` trio
that was 95% identical. Collapsed into `http_client.go` (~80 lines).
Behavior is unchanged byte-for-byte; the four provider files
shrink by 178 lines net.

### New tool: `cmd/capstan-bench`

Measures provider API latency for the operations downstream consumers
(dew, Marina-via-dew) call on every refresh, so architecture
decisions are data-driven rather than asserted. Suite covers:

- Reads: `Regions`, `Plans`, `List`, `Get`, parallel-`Get` fanout
  at configurable concurrency levels
- Mutations (`--include-mutations`): `Create` ack + visible-in-list +
  status-running, `PowerOff/PowerOn/Restart` ack + WaitForAction,
  `Destroy` ack. Costs ~one server-hour; the harness always destroys
  in a deferred cleanup
- Outputs Markdown summary table to stdout and optional per-call
  NDJSON to `--raw` for post-hoc analysis
- Auto-fallback ladder on placement / deprecated failures (Hetzner
  cax11 placement-unavailable at FRA is common; default changed
  cax11 → cx23)

Token resolution accepts every common alias per provider:

```
Hetzner       HCLOUD_TOKEN | HETZNER_API_TOKEN | HETZNER_TOKEN
DigitalOcean  DIGITALOCEAN_TOKEN | DIGITALOCEAN_ACCESS_TOKEN |
              DOCTL_ACCESS_TOKEN | DO_API_KEY | DO_TOKEN
Linode        LINODE_TOKEN | LINODE_CLI_TOKEN
Vultr         VULTR_API_KEY | VULTR_TOKEN
```

### New tool: `cmd/capstan-spec-check`

Compares the embedded `specs/<provider>.json` priceCents map against
the live catalog API and reports drift:

- entries in spec but no longer returned by the API (likely deprecated)
- entries in the live API but missing from spec (new types to add)

Linode and Vultr catalog endpoints are public — spec-check runs
against them with no token. Hetzner and DigitalOcean require auth.
Exit 0 in sync, 1 on drift, 2 on error.

First sweep against all four specs (2026-05-29) surfaced significant
drift:

```
Hetzner      19 in sync, 4 deprecated, 6 new   (fixed in this release)
DigitalOcean 10 in sync, 0 deprecated, 162 new (verified with token)
Linode       12 in sync, 0 deprecated, 63 new  (public, no token)
Vultr         9 in sync, 1 deprecated, 142 new (public, no token)
```

### `specs/hetzner.json` brought back in sync

- Removed: `cx22`, `cx32`, `cx42`, `cx52` (Hetzner reports `server type
  N is deprecated` on Create)
- Added: `cpx12` (Singapore only, €9.49), `cpx22`, `cpx32`, `cpx42`,
  `cpx52`, `cpx62` (cpx V2 generation, AMD EPYC, fsn1 pricing)
- Price corrections — `ccx` dedicated tier has had significant
  increases since the previous spec snapshot:

  ```
  ccx13  1449 → 1849   (+27%)
  ccx23  2599 → 3699   (+42%)
  ccx33  4849 → 7399   (+53%)
  ccx43  9199 → 14749  (+60%)
  ccx53 17399 → 29499  (+70%)
  ccx63 33599 → 44199  (+32%)
  ```

After this fix, `capstan-spec-check --provider hetzner` exits 0
against the live API (25 types in sync).

### CI: weekly `spec-drift` workflow

`.github/workflows/spec-drift.yml` runs `capstan-spec-check` against
each provider on a Monday cron, on push to main affecting `specs/` or
the tool, on PRs, and on manual dispatch. Linode and Vultr always run
(public catalog); Hetzner and DigitalOcean run when the corresponding
secret is set (`HCLOUD_TOKEN`, `DO_API_KEY` — repo or org level).
Schedule runs are informational (warning, never red-badges); PR/push
runs hard-fail on drift to catch broken specs before merge.

### Tests

Go test count grew from 13 to ~50: 8 new Hetzner tests for the new
methods, 3 factory tests, 5 stats tests, 7 bench tests (token
resolution, alias fallback, fallback ladder coverage, retryable-error
classification), 5 spec-check tests (in-sync / deprecated-in-spec /
new-in-API / both-sides / token alias). All green.

## v0.3.0 — `recommendPlacement` posture module

Adds a new `posture` module — a pure-function placement recommender that
maps `(geography, workload, slaTier)` to an ordered `(provider, region, size)`
recommendation plus operational caveats.

```ts
import { recommendPlacement } from 'capstan'

const r = recommendPlacement({
  geography: 'japan',
  workload: 'cpu-latency',
  sla: 'standard',
})
// r.primary    = { provider: 'linode', region: 'ap-northeast', size: 'g7-premium-4' }
// r.fallbacks  = [ Linode jp-tyo-3, Linode jp-osa ]
// r.caveats    = ['Linode g7-premium has elevated SSH-provisioning ...']
```

14 customer geographies × 4 workload classes × 3 SLA tiers = 168 placement
decisions encoded. Caveats surface known operational gotchas (Linode Sydney
structural slowness, Linode g7 reliability budget, Hetzner Singapore pricing
premium without perf gain, ash placement-unavailability hot windows).

### Why this exists

Capstan abstracts provider APIs but doesn't say which provider+region+size
a given workload should go to. That decision used to live in implicit
operator knowledge or ad-hoc per-project tables. With per-region L3 perf
characterization (see internal `lab/HETZNER-REGIONS-MATRIX.md` and
`lab/LINODE-REGIONS-MATRIX.md`) we now have the data to encode it as
a function.

The module is pure data + decision logic; it does not call any provider
API. Live availability (placement 412s, region restrictions, account
gating) is a separate concern — use the returned `fallbacks` array as
the retry ladder when `createVPS()` fails with a retryable error.

Data tables snapshot: 2026-05-22. Re-verify quarterly.

### Subpath import

```ts
import { recommendPlacement } from 'capstan/posture'
```

Type re-exports: `Geography`, `Workload`, `SlaTier`, `PostureInput`,
`Placement`, `Recommendation`.

## v0.2.1 — `image` field on `EphemeralSessionOptions`

Adds an `image` field to `EphemeralSessionOptions` so callers can
override the OS image when opening an ephemeral session. Previously
`openEphemeralSession()` always took the provider's built-in default
(Ubuntu LTS); workloads that benchmark across kernels or need a
specific base image had to bypass the helper.

No breaking changes. The field is optional — existing callers keep
the previous behavior.

### Usage

```ts
await using session = await openEphemeralSession(provider, {
  name: 'creek-bench',
  size: 'ccx33',
  region: 'fsn1',
  publicKey: pubKey,
  image: 'debian-12',  // new — falls back to provider default if omitted
})
```

Accepted values are whatever the underlying provider's `createVPS()`
accepts — `'ubuntu-24.04'`, `'debian-12'`, `'fedora-40'`, etc. on
Hetzner; their respective ids on DigitalOcean / Linode / Vultr. See
each provider's `DEFAULT_IMAGE` constant in `src/<provider>.ts` for
the current fallback.

### Why this exists

Cross-kernel SQLite benchmarks on Hetzner ccx33 found OS image is
the dominant tunable for the workload — debian-12 (kernel 6.1)
outperforms ubuntu-24.04 (kernel 6.8) by ~10% on root fs and ~50%
on loop-mounted ext4, same hardware. Capstan needed to expose that
knob through the ephemeral-session helper so callers don't have to
fall back to raw `createVPS()` to set it.

### Tests

2 new unit tests in `test/session.test.ts` — one for the field
being forwarded when set, one confirming it's omitted from the
`createVPS()` call when not set (so the provider's default
behavior is unchanged).

Total test count: 125 (was 123).

## v0.2.0 — Ephemeral session helper

Adds `openEphemeralSession()` — a small helper that collapses the
~30 lines of resource-tracking boilerplate every lab script and
bench tool was repeating into a single `await using` block.

No breaking changes. The existing `Provider` interface, all four
provider implementations, and the registry are unchanged.

### New: `openEphemeralSession(provider, opts)`

```ts
import { openEphemeralSession, HetznerProvider } from 'capstan'

const provider = new HetznerProvider({ token: process.env.HETZNER_API_TOKEN! })

await using session = await openEphemeralSession(provider, {
  name: `bench-${Date.now()}`,
  size: 'cx43',
  region: 'fsn1',
  publicKey: myPublicKey,
  userData: '#cloud-config\n...',
})

const ip = await session.publicIP()
// ... ssh root@ip, run work ...
// On scope exit: VPS destroyed, SSH key deleted, automatically.
```

What the helper handles:

- Uploads the SSH key, then creates the VPS. If `createVPS()` fails,
  the SSH key is rolled back so failed provisioning attempts don't
  leak keys on the provider account.
- `session.publicIP()` returns immediately if the provider gave back
  an IP from `createVPS()` (Hetzner / Linode / Vultr behavior).
  Otherwise polls `provider.getVPS()` every 2s for up to 60s
  (DigitalOcean behavior). Result is cached so repeated calls
  don't re-poll.
- `session.dispose()` (and `[Symbol.asyncDispose]`) destroys the VPS
  first to release the public IP, then deletes the SSH key. Both
  steps are best-effort and never throw — a failure in one step
  doesn't skip the other, and a dispose during a `try/finally`
  doesn't mask the caller's original error.
- Idempotent: `dispose()` can be called multiple times safely.

### Requirements

- Node 22+ (`Symbol.asyncDispose` runtime support — already required
  by capstan's `engines.node`)
- TypeScript 5.2+ for `await using` syntax — older TS can still use
  the helper via explicit `await session.dispose()` in a `finally`.

### Why this exists

Every lab script and bench tool consuming capstan was repeating the
same shape:

```ts
let createdKey = null
let createdVPS = null
const teardown = async () => { /* ~10 lines */ }
process.on('SIGINT', async () => { await teardown(); process.exit(130) })
createdKey = await provider.uploadSSHKey({ ... })
createdVPS = await provider.createVPS({ ... })
let ip = createdVPS.publicIPv4
if (!ip) { /* ~10 lines of polling */ }
try { /* the actual work */ } finally { await teardown() }
```

That's ~30 lines of boilerplate before the script even gets to its
own logic. The session helper drops it to ~5 lines and removes
several common bugs (forgetting `process.on('SIGINT')`, double-
destroy, key leaks on partial provisioning failure).

### Tests

16 new unit tests in `test/session.test.ts` against a mock provider —
covers the happy path, partial-failure rollback, IP polling with
configurable delay, idempotent dispose, `Symbol.asyncDispose`, and
`await using` syntax end-to-end.

Total test count: 123 (was 107).

## v0.1.0 — Initial extraction from groundflare

First public release. Multi-provider VPS lifecycle library extracted
from groundflare so labs and orchestrators can consume the typed
`Provider` interface without inheriting groundflare's workerd-specific
weight.

- Four provider implementations: Hetzner, DigitalOcean, Linode, Vultr
- Typed `Provider` interface across all four
- Provider registry with `createProvider(name, opts)`
- Normalized `ProviderError` with HTTP status, code, and retryable flag
- Monthly cost estimation per (size, region)
- 107 unit tests, all passing
- Published via GitHub Actions OIDC Trusted Publisher with SLSA
  provenance attestation
