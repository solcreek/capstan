# Changelog

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
