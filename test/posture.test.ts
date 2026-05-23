import { describe, expect, it } from 'vitest'
import {
  recommendPlacement,
  type Geography,
  type Workload,
  type SlaTier,
} from '../src/posture.js'

describe('recommendPlacement — geography routing', () => {
  it('EU central → Hetzner fsn1 as primary', () => {
    const r = recommendPlacement({ geography: 'eu-central' })
    expect(r.primary.provider).toBe('hetzner')
    expect(r.primary.region).toBe('fsn1')
  })

  it('EU north → Hetzner hel1 as primary, Linode Stockholm as fallback', () => {
    const r = recommendPlacement({ geography: 'eu-north' })
    expect(r.primary).toMatchObject({ provider: 'hetzner', region: 'hel1' })
    expect(r.fallbacks).toContainEqual(
      expect.objectContaining({ provider: 'linode', region: 'se-sto' }),
    )
  })

  it('US west → Hetzner hil as primary (global price/perf winner)', () => {
    const r = recommendPlacement({ geography: 'us-west' })
    expect(r.primary).toMatchObject({ provider: 'hetzner', region: 'hil' })
  })

  it('Japan → Linode ap-northeast (APAC anchor)', () => {
    const r = recommendPlacement({ geography: 'japan' })
    expect(r.primary).toMatchObject({ provider: 'linode', region: 'ap-northeast' })
  })

  it('Singapore → Hetzner sin with pricing caveat surfaced', () => {
    const r = recommendPlacement({ geography: 'singapore' })
    expect(r.primary).toMatchObject({ provider: 'hetzner', region: 'sin' })
    expect(r.caveats.join(' ')).toMatch(/Singapore.*\+55%/)
  })

  it('LATAM → Linode br-gru', () => {
    const r = recommendPlacement({ geography: 'latam' })
    expect(r.primary).toMatchObject({ provider: 'linode', region: 'br-gru' })
  })

  it('India → Linode in-bom-2 as primary', () => {
    const r = recommendPlacement({ geography: 'india' })
    expect(r.primary).toMatchObject({ provider: 'linode', region: 'in-bom-2' })
  })

  it('Indonesia → Linode id-cgk', () => {
    const r = recommendPlacement({ geography: 'indonesia' })
    expect(r.primary).toMatchObject({ provider: 'linode', region: 'id-cgk' })
  })
})

describe('recommendPlacement — Australia exception', () => {
  it('Australia → Linode ap-southeast with structural-slowness caveat', () => {
    const r = recommendPlacement({ geography: 'australia' })
    expect(r.primary).toMatchObject({ provider: 'linode', region: 'ap-southeast' })
    expect(r.caveats.join(' ')).toMatch(/Sydney is structurally slower/)
    expect(r.caveats.join(' ')).toMatch(/tier upgrades.*do not close the gap/i)
  })
})

describe('recommendPlacement — workload routing', () => {
  it('io-multitenant + standard on Hetzner → cx43', () => {
    const r = recommendPlacement({ geography: 'eu-central', workload: 'io-multitenant', sla: 'standard' })
    expect(r.primary.size).toBe('cx43')
  })

  it('io-multitenant + premium on Hetzner → ccx33', () => {
    const r = recommendPlacement({ geography: 'eu-central', workload: 'io-multitenant', sla: 'premium' })
    expect(r.primary.size).toBe('ccx33')
  })

  it('io-multitenant + best-effort on Hetzner → cx23', () => {
    const r = recommendPlacement({ geography: 'eu-central', workload: 'io-multitenant', sla: 'best-effort' })
    expect(r.primary.size).toBe('cx23')
  })

  it('cpu-throughput on Hetzner → cax (ARM has best per-vCPU)', () => {
    const r = recommendPlacement({ geography: 'eu-central', workload: 'cpu-throughput', sla: 'standard' })
    expect(r.primary.size).toBe('cax21')
  })

  it('cpu-latency on Linode → g7-premium-4 (jitter reduction tier)', () => {
    const r = recommendPlacement({ geography: 'japan', workload: 'cpu-latency', sla: 'standard' })
    expect(r.primary.size).toBe('g7-premium-4')
    expect(r.caveats.join(' ')).toMatch(/SSH-provisioning failure/)
  })

  it('cpu-throughput on Linode → g6-standard (NOT g7)', () => {
    const r = recommendPlacement({ geography: 'japan', workload: 'cpu-throughput', sla: 'standard' })
    expect(r.primary.size).toBe('g6-standard-4')
  })
})

describe('recommendPlacement — SLA tiers', () => {
  it('premium SLA on Linode picks dedicated tier (with gating caveat)', () => {
    const r = recommendPlacement({ geography: 'japan', workload: 'io-multitenant', sla: 'premium' })
    expect(r.primary.size).toBe('g6-dedicated-4')
    expect(r.caveats.join(' ')).toMatch(/Support approval/)
  })

  it('best-effort SLA picks cheapest viable tier', () => {
    const r = recommendPlacement({ geography: 'japan', workload: 'io-multitenant', sla: 'best-effort' })
    expect(r.primary.size).toBe('g6-nanode-1')
  })
})

describe('recommendPlacement — fallbacks ordering', () => {
  it('returns fallbacks in catalog priority order', () => {
    const r = recommendPlacement({ geography: 'us-west', sla: 'standard' })
    expect(r.primary.provider).toBe('hetzner')
    // First fallback is Linode us-west, then us-lax, then us-sea
    expect(r.fallbacks[0]).toMatchObject({ provider: 'linode', region: 'us-west' })
    expect(r.fallbacks[1]).toMatchObject({ provider: 'linode', region: 'us-lax' })
  })

  it('Sydney has no fallback (only AU option today)', () => {
    const r = recommendPlacement({ geography: 'australia' })
    expect(r.fallbacks).toHaveLength(0)
  })
})

describe('recommendPlacement — defaults', () => {
  it('omitting workload defaults to io-multitenant', () => {
    const r = recommendPlacement({ geography: 'eu-central' })
    expect(r.primary.size).toBe('cx43') // io-multitenant + standard
  })

  it('omitting sla defaults to standard', () => {
    const r = recommendPlacement({ geography: 'eu-central', workload: 'io-multitenant' })
    expect(r.primary.size).toBe('cx43')
  })
})

describe('recommendPlacement — type completeness', () => {
  // Compile-time guarantee: every Geography has at least one candidate.
  // Runtime check: walk every geography × workload × sla and verify
  // no path throws.
  const ALL_GEOS: readonly Geography[] = [
    'eu-central', 'eu-north', 'eu-west', 'eu-south',
    'us-east', 'us-central', 'us-west', 'canada',
    'singapore', 'japan', 'india', 'indonesia', 'australia', 'latam',
  ]
  const ALL_WORKLOADS: readonly Workload[] = [
    'io-multitenant', 'cpu-latency', 'cpu-throughput', 'general',
  ]
  const ALL_SLAS: readonly SlaTier[] = ['best-effort', 'standard', 'premium']

  for (const g of ALL_GEOS) {
    for (const w of ALL_WORKLOADS) {
      for (const s of ALL_SLAS) {
        it(`(${g}, ${w}, ${s}) returns a placement`, () => {
          const r = recommendPlacement({ geography: g, workload: w, sla: s })
          expect(r.primary.provider).toBeDefined()
          expect(r.primary.region).toBeTruthy()
          expect(r.primary.size).toBeTruthy()
        })
      }
    }
  }
})
