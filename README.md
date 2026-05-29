# capstan

> Multi-provider VPS lifecycle library — Hetzner, DigitalOcean, Linode, Vultr. TypeScript + Go, one shared spec.

```bash
npm i capstan            # TypeScript
go get github.com/solcreek/capstan  # Go
```

## Quick start

### TypeScript

```ts
import { HetznerProvider } from 'capstan'

const p = new HetznerProvider({ token: process.env.HETZNER_API_TOKEN! })

const vps = await p.createVPS({
  name: 'my-server',
  size: 'cx33',
  region: 'fsn1',
  sshKeyIds: [(await p.uploadSSHKey({ name: 'mykey', publicKey })).id],
  userData: '#cloud-config\nruncmd:\n  - echo hello\n',
})

await p.destroyVPS(vps.id)
```

### Go

```go
import "github.com/solcreek/capstan"

spec := capstan.Spec(capstan.Hetzner)
fmt.Println(spec.BaseURL)           // https://api.hetzner.cloud/v1
fmt.Println(spec.MapStatus("running")) // running
fmt.Println(spec.EstimateMonthlyCost("cx23")) // 499

r, _ := capstan.RecommendPlacement("us-east", "io-multitenant", "standard")
fmt.Println(r.Primary) // {hetzner ash cx43}
```

Same provider, different language:

```ts
import { createProvider } from 'capstan/registry'
const p = createProvider('digitalocean', { token: process.env.DO_TOKEN! })
```

## Architecture

```
specs/                     ← single source of truth (JSON)
├── hetzner.json              base URL, status map, pricing
├── digitalocean.json
├── linode.json
├── vultr.json
└── posture.json              geo routing, tier map, caveats

src/                       ← TypeScript implementation (npm)
  imports specs/*.json

*.go                       ← Go implementation (//go:embed specs/)
  imports specs/*.json
```

Provider metadata lives in `specs/*.json`. Both languages read the same files — update a spec, both implementations stay in sync.

## What it is

One typed `Provider` interface with four working backends. Every implementation:

- Authenticates via API token
- Lists sizes and regions with pricing in cents
- Uploads / lists / deletes SSH keys
- Creates / gets / lists / destroys VPSes with cloud-init `userData`
- Surfaces a normalized error type (with `code`, `status`, `retryable`)
- Quotes monthly cost via `estimateMonthlyCost()`

## What it isn't

- **Not Terraform / Pulumi.** No state file, no diff, no plan. Imperative provider calls.
- **Not a CLI.** Library only.
- **Not a deployment tool.** Provisions the box. What runs on it is your concern.

## Providers

| Provider | Account | Sizes/Regions | SSH keys | VPS lifecycle | User-data | Tests |
|---|---|---|---|---|---|---|
| **Hetzner** | ✓ | ✓ | ✓ | ✓ | ✓ | 33 |
| **DigitalOcean** | ✓ | ✓ | ✓ | ✓ | ✓ | 17 |
| **Linode** | ✓ | ✓ | ✓ | ✓ | ✓ | 22 |
| **Vultr** | ✓ | ✓ | ✓ | ✓ | ✓ | 27 |

~360 tests total (314 TypeScript + ~50 Go). No network calls in tests — providers accept a `fetchImpl` injection (TS) and specs are embedded (Go).

## Provider interface (TypeScript)

```ts
interface Provider {
  readonly name: ProviderName
  readonly displayName: string

  authenticate(token: string): Promise<Account>

  listSizes(region?: string): Promise<readonly Size[]>
  listRegions(): Promise<readonly Region[]>

  uploadSSHKey(opts: SSHKeyOptions): Promise<SSHKey>
  listSSHKeys(): Promise<readonly SSHKey[]>
  deleteSSHKey(id: string): Promise<void>

  createVPS(opts: ProvisionOptions): Promise<VPS>
  getVPS(id: string): Promise<VPS | null>
  listVPS(): Promise<readonly VPS[]>
  destroyVPS(id: string): Promise<void>

  estimateMonthlyCost(opts: { size: string; region: string }): number
}
```

See [`src/types.ts`](./src/types.ts) for all value types.

## Go interface (v0.5+)

```go
type Provider interface {
    Name() ProviderName
    Regions(ctx) ([]Region, error)
    Plans(ctx, region) ([]Plan, error)

    List(ctx, ListOpts) ([]Server, error)
    Get(ctx, id) (*Server, error)

    Create(ctx, CreateOpts) (*Server, error)
    Destroy(ctx, id) error

    PowerOn(ctx, id) (*Action, error)
    PowerOff(ctx, id) (*Action, error)
    Restart(ctx, id) (*Action, error)
    WaitForAction(ctx, actionID) (*Action, error)

    EstimateMonthlyCost(plan) int
}

func New(name ProviderName, token string) (Provider, error)
func Spec(name ProviderName) *ProviderSpec
func RecommendPlacement(geography, workload, sla) (*Recommendation, error)
```

Hetzner is the reference for the v0.5 methods; DigitalOcean / Linode /
Vultr return `ErrNotImplemented` for `List` / `PowerOn/Off/Restart` /
`WaitForAction` until a real consumer drives that work.

## Tools (in this repo)

| Tool | Purpose |
|---|---|
| `cmd/capstan-bench` | Measure provider API latency end-to-end (reads, fanout, full mutation lifecycle). Used to settle architecture choices with data |
| `cmd/capstan-spec-check` | Compare embedded `specs/<provider>.json` against the live provider catalog; report drift |

`spec-drift` runs weekly via GitHub Actions. Linode and Vultr catalog
endpoints are public so those checks need no secrets; Hetzner and
DigitalOcean run when `HCLOUD_TOKEN` / `DO_API_KEY` is configured at
repo or org level. Read-only tokens are sufficient.

## Ephemeral sessions (TypeScript, v0.2+)

For scripts that spin up a VPS, do work, and tear down — `openEphemeralSession()` handles SSH key upload, VPS creation, IP polling, and cleanup via `await using`:

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
// ... ssh root@ip, run your work ...
// On scope exit: VPS destroyed, SSH key deleted.
```

Requires Node 22+ and TypeScript 5.2+.

## Placement posture (v0.3+)

Given a customer geography + workload class + SLA tier, returns an ordered (provider, region, size) recommendation:

```ts
import { recommendPlacement } from 'capstan'

const r = recommendPlacement({
  geography: 'japan',
  workload: 'cpu-latency',
  sla: 'standard',
})
// r.primary   = { provider: 'linode', region: 'ap-northeast', size: 'g7-premium-4' }
// r.fallbacks = [jp-tyo-3, jp-osa]
// r.caveats   = ['Linode g7-premium has elevated SSH-provisioning ...']
```

14 geographies × 4 workloads × 3 SLA tiers = 168 encoded placement decisions. Data tables live in `specs/posture.json`. Same function available in Go via `capstan.RecommendPlacement()`.

## Roadmap

- `0.1` — provider abstraction
- `0.2` — ephemeral session helper
- `0.3` — placement posture + shared JSON specs + Go module
- `0.4` — Go HTTP client (create/destroy/get VPS via provider APIs)
- `0.5` — Go `Provider` extended with `List` + power actions;
  `New(name, token)` factory; shared `httpClient`; `cmd/capstan-bench`
  + `cmd/capstan-spec-check`; weekly drift CI; Hetzner spec re-synced
  to current Cloud API (current)

## Why "capstan"?

A capstan is the rotating drum on a ship used to hoist heavy things — anchors, sails, cables. This library hoists servers up and down.

## License

MIT. See LICENSE.
