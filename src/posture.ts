/**
 * Provider posture: pure-function placement recommender.
 *
 * Data tables live in specs/posture.json (single source of truth for
 * both TypeScript and Go consumers). This module loads the JSON and
 * provides the typed recommendPlacement() API.
 */
import type { ProviderName } from './types.js'
import postureSpec from '../specs/posture.json' with { type: 'json' }

// ─── Public types ──────────────────────────────────────────────────

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

export type Workload =
  | 'io-multitenant'
  | 'cpu-latency'
  | 'cpu-throughput'
  | 'general'

export type SlaTier =
  | 'best-effort'
  | 'standard'
  | 'premium'

export interface PostureInput {
  readonly geography: Geography
  readonly workload?: Workload
  readonly sla?: SlaTier
}

export interface Placement {
  readonly provider: ProviderName
  readonly region: string
  readonly size: string
}

export interface Recommendation {
  readonly primary: Placement
  readonly fallbacks: readonly Placement[]
  readonly caveats: readonly string[]
}

// ─── Internal data tables (loaded from specs/posture.json) ─────────

interface RegionCandidate {
  readonly provider: ProviderName
  readonly region: string
}

const GEO_ROUTING = postureSpec.geoRouting as Record<Geography, readonly RegionCandidate[]>
const TIER_MAP = postureSpec.tierMap as Record<ProviderName, Record<Workload, Record<SlaTier, string>>>
const CELL_CAVEATS = postureSpec.cellCaveats as Record<string, readonly string[]>
const SIZE_CAVEATS = postureSpec.sizeCaveats as Record<string, readonly string[]>

// ─── Public API ────────────────────────────────────────────────────

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
