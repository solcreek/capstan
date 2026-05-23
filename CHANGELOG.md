# Changelog

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
