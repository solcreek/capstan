/**
 * Provider posture: pure-function placement recommender.
 *
 * Given a customer geography (and optionally a workload class + SLA tier),
 * returns an ordered (provider, region, size) recommendation plus any
 * caveats the caller should surface.
 *
 * This module is **static catalog + decision logic only**. It does not
 * call any provider API. Live placement availability is a separate
 * concern; this just answers "where SHOULD a workload go" based on
 * benchmarked perf, pricing, and known operational gotchas.
 *
 * Data tables snapshot: 2026-05-22. Re-verify quarterly — provider
 * pricing, region availability, and tier characteristics drift.
 */
import type { ProviderName } from './types.js'

// ─── Public types ──────────────────────────────────────────────────

/**
 * Customer geography. Coarse-grained on purpose — finer routing (e.g.
 * SE-Asia vs Indonesia vs Singapore) maps to specific Geography values.
 */
export type Geography =
  | 'eu-central'
  | 'eu-north'
  | 'eu-west'
  | 'eu-south'
  | 'us-east'
  | 'us-central'
  | 'us-west'
  | 'canada'
  | 'singapore'
  | 'japan'
  | 'india'
  | 'indonesia'
  | 'australia'
  | 'latam'

/**
 * Workload class. Drives tier selection.
 *
 * - `io-multitenant`: SQLite/Postgres tenant packing, fsync-heavy.
 *   The default for Creek's core hosted offering.
 * - `cpu-latency`: real-time API, interactive UI backend.
 *   Jitter-sensitive; tail latency matters more than throughput.
 * - `cpu-throughput`: batch, encoding, sustained compute.
 *   Throughput per dollar dominates.
 * - `general`: mixed or unknown. Picks a reasonable middle ground.
 */
export type Workload =
  | 'io-multitenant'
  | 'cpu-latency'
  | 'cpu-throughput'
  | 'general'

/**
 * SLA tier. Drives shared-vs-dedicated and premium-vs-standard choice.
 */
export type SlaTier =
  | 'best-effort'   // hobby, side projects
  | 'standard'      // typical production
  | 'premium'       // SLA-bearing, prefer dedicated CPU + reliability budget

export interface PostureInput {
  readonly geography: Geography
  /** Defaults to `io-multitenant` (Creek's core case). */
  readonly workload?: Workload
  /** Defaults to `standard`. */
  readonly sla?: SlaTier
}

export interface Placement {
  readonly provider: ProviderName
  readonly region: string
  readonly size: string
}

export interface Recommendation {
  /** Best pick given the inputs. */
  readonly primary: Placement
  /**
   * Ordered fallbacks. Use when the primary's `createVPS` fails with a
   * retryable error (placement unavailability, region quota, etc.).
   */
  readonly fallbacks: readonly Placement[]
  /** Caveats the caller should log or surface to the customer. */
  readonly caveats: readonly string[]
}

// ─── Internal data tables ──────────────────────────────────────────

interface RegionCandidate {
  readonly provider: ProviderName
  readonly region: string
}

/**
 * Geography → ordered preferred regions across all providers.
 *
 * Ordering reflects the strategy posture (2026-05-22):
 *   - Hetzner first wherever it has a PoP (price/perf advantage)
 *   - Linode regions to fill geographies Hetzner can't reach
 *   - Within a provider, prefer newer-generation DCs
 */
const GEO_ROUTING: Record<Geography, readonly RegionCandidate[]> = {
  'eu-central': [
    { provider: 'hetzner', region: 'fsn1' },
    { provider: 'hetzner', region: 'nbg1' },
    { provider: 'linode', region: 'de-fra-2' },
    { provider: 'linode', region: 'eu-central' },
    { provider: 'linode', region: 'nl-ams' },
  ],
  'eu-north': [
    { provider: 'hetzner', region: 'hel1' },
    { provider: 'linode', region: 'se-sto' },
  ],
  'eu-west': [
    { provider: 'linode', region: 'gb-lon' },
    { provider: 'linode', region: 'eu-west' },
    { provider: 'linode', region: 'fr-par-2' },
    { provider: 'linode', region: 'fr-par' },
  ],
  'eu-south': [
    { provider: 'linode', region: 'it-mil' },
    { provider: 'linode', region: 'es-mad' },
  ],
  'us-east': [
    { provider: 'hetzner', region: 'ash' },
    { provider: 'linode', region: 'us-iad-2' },
    { provider: 'linode', region: 'us-east' },
    { provider: 'linode', region: 'us-iad' },
  ],
  'us-central': [
    { provider: 'linode', region: 'us-ord' },
    { provider: 'linode', region: 'us-central' },
  ],
  'us-west': [
    // hil ccx33 is the global price/perf leader for I/O-multitenant.
    { provider: 'hetzner', region: 'hil' },
    { provider: 'linode', region: 'us-west' },
    { provider: 'linode', region: 'us-lax' },
    { provider: 'linode', region: 'us-sea' },
  ],
  'canada': [
    { provider: 'linode', region: 'ca-central' },
  ],
  'singapore': [
    { provider: 'hetzner', region: 'sin' },
    { provider: 'linode', region: 'sg-sin-2' },
    { provider: 'linode', region: 'ap-south' },
  ],
  'japan': [
    // ap-northeast is the APAC anchor on benchmark.
    { provider: 'linode', region: 'ap-northeast' },
    { provider: 'linode', region: 'jp-tyo-3' },
    { provider: 'linode', region: 'jp-osa' },
  ],
  'india': [
    { provider: 'linode', region: 'in-bom-2' },
    { provider: 'linode', region: 'ap-west' },
    { provider: 'linode', region: 'in-maa' },
  ],
  'indonesia': [
    { provider: 'linode', region: 'id-cgk' },
  ],
  'australia': [
    // Sydney is the only AU option today; surfaces a caveat below.
    { provider: 'linode', region: 'ap-southeast' },
  ],
  'latam': [
    { provider: 'linode', region: 'br-gru' },
  ],
}

/**
 * (provider, workload, sla) → tier size.
 *
 * Best-effort, standard, premium are ordered cost ladders; premium
 * always picks a dedicated/guaranteed-CPU tier where available.
 */
const TIER_MAP: Record<
  ProviderName,
  Record<Workload, Record<SlaTier, string>>
> = {
  hetzner: {
    'io-multitenant': {
      'best-effort': 'cx23',
      'standard': 'cx43',
      'premium': 'ccx33',
    },
    'cpu-latency': {
      'best-effort': 'cpx21',
      'standard': 'ccx13',
      'premium': 'ccx33',
    },
    'cpu-throughput': {
      // CAX (ARM) has the best per-vCPU CPU on Hetzner.
      'best-effort': 'cax11',
      'standard': 'cax21',
      'premium': 'ccx33',
    },
    'general': {
      'best-effort': 'cx23',
      'standard': 'cx43',
      'premium': 'ccx33',
    },
  },
  linode: {
    'io-multitenant': {
      'best-effort': 'g6-nanode-1',
      'standard': 'g6-standard-4',
      // g6-dedicated may require Support approval on new accounts;
      // see caveat below.
      'premium': 'g6-dedicated-4',
    },
    'cpu-latency': {
      'best-effort': 'g6-standard-2',
      // g7-premium delivers reduced p99 jitter under CPU load; the
      // tier's value vs g6-standard shows up on tail latency, not mean.
      'standard': 'g7-premium-4',
      'premium': 'g7-premium-4',
    },
    'cpu-throughput': {
      // g7's premium pricing doesn't deliver throughput; use g6.
      'best-effort': 'g6-standard-2',
      'standard': 'g6-standard-4',
      'premium': 'g6-standard-6',
    },
    'general': {
      'best-effort': 'g6-nanode-1',
      'standard': 'g6-standard-4',
      'premium': 'g6-dedicated-4',
    },
  },
  digitalocean: {
    'io-multitenant': {
      'best-effort': 's-1vcpu-1gb',
      'standard': 's-2vcpu-4gb',
      'premium': 's-4vcpu-8gb',
    },
    'cpu-latency': {
      'best-effort': 's-1vcpu-1gb',
      'standard': 'c-2',
      'premium': 'c-4',
    },
    'cpu-throughput': {
      'best-effort': 's-1vcpu-1gb',
      'standard': 'c-2',
      'premium': 'c-4',
    },
    'general': {
      'best-effort': 's-1vcpu-1gb',
      'standard': 's-2vcpu-4gb',
      'premium': 's-4vcpu-8gb',
    },
  },
  vultr: {
    'io-multitenant': {
      'best-effort': 'vc2-1c-1gb',
      'standard': 'vc2-2c-4gb',
      'premium': 'vc2-4c-8gb',
    },
    'cpu-latency': {
      'best-effort': 'vc2-1c-1gb',
      'standard': 'vhf-2c-4gb',
      'premium': 'vhf-4c-8gb',
    },
    'cpu-throughput': {
      'best-effort': 'vc2-1c-1gb',
      'standard': 'vhf-2c-4gb',
      'premium': 'vhf-4c-8gb',
    },
    'general': {
      'best-effort': 'vc2-1c-1gb',
      'standard': 'vc2-2c-4gb',
      'premium': 'vc2-4c-8gb',
    },
  },
}

/**
 * Per-cell caveats. Surfaces operational gotchas the caller may need
 * to log or relay to a customer. Keyed by `${provider}/${region}`.
 */
const CELL_CAVEATS: Record<string, readonly string[]> = {
  'linode/ap-southeast': [
    'Linode Sydney is structurally slower than other regions on the same SKU (~47k vs ~80k ops/s for SQLite multi-tenant writes). Tier upgrades — g6-dedicated, g6-standard-6, g7/g8 — do not close the gap. Newer-gen tiers are not yet available in this region. For latency-tolerant workloads, consider routing to Tokyo + cross-Pacific RTT.',
  ],
  'hetzner/sin': [
    'Hetzner Singapore charges ~+55% vs EU central pricing for ~17% lower SQLite-class throughput. Pay for presence, not performance.',
  ],
  'hetzner/ash': [
    'Hetzner ccx33 in ash has shown elevated 412 placement-unavailable rates during US business hours. Build retry-with-fallback to hil.',
  ],
}

/**
 * Per-(provider, size) caveats — surfaces tier-level gotchas regardless
 * of region.
 */
const SIZE_CAVEATS: Record<string, readonly string[]> = {
  'linode/g6-dedicated-4': [
    'Linode g6-dedicated-* may require Support approval on new accounts. If createInstance returns 400 plan-limit, open a Support ticket and retry within ~24h.',
  ],
  'linode/g7-premium-4': [
    'Linode g7-premium has elevated SSH-provisioning failure rates (17-50% depending on region). Build retry-with-backoff allowing 2-3× provisioning attempts.',
  ],
}

// ─── Public API ────────────────────────────────────────────────────

/**
 * Recommend a (provider, region, size) placement for a customer geography.
 *
 * Pure function over the static catalog. Does not query live provider
 * availability. Use the returned `fallbacks` array as the retry ladder
 * when `createVPS` fails with a retryable error.
 */
export function recommendPlacement(input: PostureInput): Recommendation {
  const workload = input.workload ?? 'io-multitenant'
  const sla = input.sla ?? 'standard'

  const candidates = GEO_ROUTING[input.geography]
  const placements: Placement[] = candidates.map((c) => ({
    provider: c.provider,
    region: c.region,
    size: TIER_MAP[c.provider][workload][sla],
  }))

  const primary = placements[0]
  if (primary === undefined) {
    throw new Error(`No regions catalogued for geography "${input.geography}"`)
  }
  const fallbacks = placements.slice(1)
  const caveats = collectCaveats(primary)

  return { primary, fallbacks, caveats }
}

function collectCaveats(placement: Placement): readonly string[] {
  const cellKey = `${placement.provider}/${placement.region}`
  const sizeKey = `${placement.provider}/${placement.size}`
  return [...(CELL_CAVEATS[cellKey] ?? []), ...(SIZE_CAVEATS[sizeKey] ?? [])]
}
