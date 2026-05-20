# capstan

> Multi-provider VPS lifecycle library — Hetzner, DigitalOcean, Linode, Vultr behind one TypeScript interface.

```bash
npm i capstan
```

```ts
import { HetznerProvider } from 'capstan'

const p = new HetznerProvider({ token: process.env.HETZNER_API_TOKEN! })

// Provision
const vps = await p.createVPS({
  name: 'my-server',
  size: 'cx33',
  region: 'fsn1',
  sshKeyIds: [(await p.uploadSSHKey({ name: 'mykey', publicKey })).id],
  userData: '#cloud-config\nruncmd:\n  - echo hello > /work/hello\n',
})

// ... do stuff over SSH to vps.publicIPv4 ...

// Tear down
await p.destroyVPS(vps.id)
```

Same code, different provider:

```ts
import { DigitalOceanProvider, LinodeProvider, VultrProvider } from 'capstan'
// Or pick at runtime:
import { createProvider } from 'capstan/registry'
const p = createProvider('digitalocean', { token: process.env.DO_TOKEN! })
```

## What it is

One typed `Provider` interface with four working backends. Every implementation:

- Authenticates via API token; reports a normalized `Account`
- Lists `Size`s and `Region`s with monthly pricing in EUR/USD cents
- Uploads / lists / deletes SSH keys
- Creates / gets / lists / destroys VPSes with cloud-init `userData`
- Surfaces a normalized `ProviderError` (with `code`, `status`, `retryable`)
- Quotes monthly cost up-front via `estimateMonthlyCost()`

That's it. No deployment, no bootstrap-stage orchestration, no Worker semantics. Layer those on top.

## What it isn't

- **Not Terraform / Pulumi / OpenTofu.** No state file, no diff, no plan. Just imperative provider calls.
- **Not a CLI.** Library only. (CLI tools that consume capstan: [groundflare](https://github.com/solcreek/groundflare) — extracting to depend on this; future internal Creek tooling.)
- **Not a deployment tool.** It provisions the box. What you do over SSH after is your problem.

## Providers

| Provider | Account | Sizes/Regions | SSH keys | VPS lifecycle | User-data | Tests |
|---|---|---|---|---|---|---|
| **Hetzner** | ✓ | ✓ | ✓ | ✓ | ✓ | 33 |
| **DigitalOcean** | ✓ | ✓ | ✓ | ✓ | ✓ | 17 |
| **Linode** | ✓ | ✓ | ✓ | ✓ | ✓ | 22 |
| **Vultr** | ✓ | ✓ | ✓ | ✓ | ✓ | 27 |

107 unit tests, all passing. No fetch goes out during tests — providers accept a `fetchImpl` injection.

## Provider interface

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
  getVPS(id: string): Promise<VPS | null>     // null when missing
  listVPS(): Promise<readonly VPS[]>
  destroyVPS(id: string): Promise<void>

  estimateMonthlyCost(opts: { size: string; region: string }): number
}
```

See [`src/types.ts`](./src/types.ts) for the value types (`Account`, `Size`, `Region`, `SSHKey`, `VPS`, `ProvisionOptions`, `ProviderError`).

## Why "capstan"?

A capstan is the rotating drum on a ship used to hoist heavy things — anchors, sails, cables. This library hoists servers up and down. The metaphor lands.

## Roadmap

- `0.1.x` — provider abstraction (this release)
- `0.2.x` — cloud-init template helpers (when needed by a downstream)
- `0.3.x` — optional bootstrap-stage orchestrator (auth → ssh-key → provision → wait-ssh → cloud-init), lifted from groundflare
- Provider additions opportunistic — Scaleway, OVH, Backblaze Compute, Fly Machines, etc.

## License

MIT. See LICENSE.

## Credit

Extracted from [groundflare](https://github.com/solcreek/groundflare). The provider abstraction was built there, then split out to be useful beyond the "deploy a Cloudflare Worker on a VPS" use case.
