import { describe, it, expect, beforeEach, afterEach, vi } from 'vitest'
import * as cli from '../src/cli.js'

// Vitest spies on stdout/stderr per test so we can assert output without
// actually printing. process.exit is replaced with a throwing stub so
// emitError tests can assert behavior without ending the test process.

class ExitError extends Error {
  constructor(public readonly code: number) {
    super(`exit(${code})`)
  }
}

let stdoutLines: string[] = []
let stderrLines: string[] = []

beforeEach(() => {
  stdoutLines = []
  stderrLines = []
  vi.spyOn(process.stdout, 'write').mockImplementation((chunk: unknown) => {
    stdoutLines.push(String(chunk))
    return true
  })
  vi.spyOn(process.stderr, 'write').mockImplementation((chunk: unknown) => {
    stderrLines.push(String(chunk))
    return true
  })
  vi.spyOn(process, 'exit').mockImplementation(((code?: number) => {
    throw new ExitError(code ?? 0)
  }) as never)
})

afterEach(() => {
  vi.restoreAllMocks()
})

function lastJson<T = any>(): T {
  const last = stdoutLines.at(-1)
  if (!last) throw new Error('no stdout output')
  return JSON.parse(last)
}

describe('providers', () => {
  it('lists 4 providers as JSON by default', () => {
    cli.cmdProviders([])
    const out = lastJson()
    expect(out.ok).toBe(true)
    expect(out.providers).toEqual(['hetzner', 'digitalocean', 'linode', 'vultr'])
  })

  it('lists as plain text when --text passed', () => {
    cli.cmdProviders(['--text'])
    expect(stdoutLines.at(-1)?.trim()).toBe('hetzner\ndigitalocean\nlinode\nvultr')
  })
})

describe('workloads / sla-tiers / geographies', () => {
  it('workloads has 4 classes', () => {
    cli.cmdWorkloads([])
    expect(lastJson().workloads).toHaveLength(4)
  })

  it('sla-tiers has 3 tiers', () => {
    cli.cmdSlaTiers([])
    expect(lastJson().slaTiers).toEqual(['best-effort', 'standard', 'premium'])
  })

  it('geographies has 14 entries', () => {
    cli.cmdGeographies([])
    expect(lastJson().geographies).toHaveLength(14)
  })
})

describe('plans', () => {
  it('returns sorted plans from spec for hetzner', () => {
    cli.cmdPlans(['hetzner'])
    const out = lastJson()
    expect(out.ok).toBe(true)
    expect(out.provider).toBe('hetzner')
    expect(out.currency).toBe('EUR')
    expect(out.plans.length).toBeGreaterThan(20)
    expect(out.plans[0].priceMonthlyCents).toBeLessThanOrEqual(out.plans[1].priceMonthlyCents)
  })

  it('errors on unknown provider', () => {
    expect(() => cli.cmdPlans(['nonexistent'])).toThrow(ExitError)
    const err = JSON.parse(stderrLines.at(-1)!)
    expect(err.code).toBe('unknown_provider')
  })

  it('errors when provider name missing', () => {
    expect(() => cli.cmdPlans([])).toThrow(ExitError)
    const err = JSON.parse(stderrLines.at(-1)!)
    expect(err.code).toBe('missing_arg')
  })
})

describe('price', () => {
  it('returns cx23 monthly price for hetzner', () => {
    cli.cmdPrice(['hetzner', 'cx23'])
    const out = lastJson()
    expect(out.ok).toBe(true)
    expect(out.plan).toBe('cx23')
    expect(out.priceMonthlyCents).toBe(499)
    expect(out.priceCurrency).toBe('EUR')
  })

  it('errors on unknown plan with stable code', () => {
    expect(() => cli.cmdPrice(['hetzner', 'cx22-deprecated'])).toThrow(ExitError)
    const err = JSON.parse(stderrLines.at(-1)!)
    expect(err.code).toBe('unknown_plan')
  })

  it('errors when plan missing', () => {
    expect(() => cli.cmdPrice(['hetzner'])).toThrow(ExitError)
    const err = JSON.parse(stderrLines.at(-1)!)
    expect(err.code).toBe('missing_arg')
  })
})

describe('recommend', () => {
  it('returns a placement for japan/cpu-latency/standard', () => {
    cli.cmdRecommend(['--geo', 'japan', '--workload', 'cpu-latency', '--sla', 'standard'])
    const out = lastJson()
    expect(out.ok).toBe(true)
    expect(out.primary.provider).toBeTypeOf('string')
    expect(out.primary.region).toBeTypeOf('string')
    expect(out.primary.size).toBeTypeOf('string')
  })

  it('defaults workload=general and sla=standard when only --geo given', () => {
    cli.cmdRecommend(['--geo', 'eu-central'])
    expect(lastJson().ok).toBe(true)
  })

  it('errors when --geo missing', () => {
    expect(() => cli.cmdRecommend([])).toThrow(ExitError)
    const err = JSON.parse(stderrLines.at(-1)!)
    expect(err.code).toBe('missing_arg')
  })

  it('errors on unknown geography', () => {
    expect(() => cli.cmdRecommend(['--geo', 'mars'])).toThrow(ExitError)
    const err = JSON.parse(stderrLines.at(-1)!)
    expect(err.code).toBe('bad_arg')
  })
})

describe('input hardening', () => {
  it('rejects control characters in plan id', () => {
    expect(() => cli.cmdPrice(['hetzner', 'cx23\x00'])).toThrow(ExitError)
    const err = JSON.parse(stderrLines.at(-1)!)
    expect(err.code).toBe('bad_arg')
    expect(err.error).toContain('control character')
  })

  it('rejects query injection in plan id', () => {
    expect(() => cli.cmdPrice(['hetzner', 'cx23?fields=name'])).toThrow(ExitError)
    const err = JSON.parse(stderrLines.at(-1)!)
    expect(err.code).toBe('bad_arg')
  })

  it('rejects URL-encoded segment in plan id', () => {
    expect(() => cli.cmdPrice(['hetzner', '%2e%2e'])).toThrow(ExitError)
    const err = JSON.parse(stderrLines.at(-1)!)
    expect(err.code).toBe('bad_arg')
  })

  it('rejects whitespace in plan id', () => {
    expect(() => cli.cmdPrice(['hetzner', 'cx 23'])).toThrow(ExitError)
    expect(JSON.parse(stderrLines.at(-1)!).code).toBe('bad_arg')
  })

  it('rejects control char in geo', () => {
    expect(() => cli.cmdRecommend(['--geo', 'japan\x01'])).toThrow(ExitError)
    expect(JSON.parse(stderrLines.at(-1)!).code).toBe('bad_arg')
  })

  it('rejects unknown workload', () => {
    expect(() => cli.cmdRecommend(['--geo', 'japan', '--workload', 'fake'])).toThrow(ExitError)
    expect(JSON.parse(stderrLines.at(-1)!).code).toBe('bad_arg')
  })

  it('rejects unknown sla', () => {
    expect(() => cli.cmdRecommend(['--geo', 'japan', '--sla', 'fake'])).toThrow(ExitError)
    expect(JSON.parse(stderrLines.at(-1)!).code).toBe('bad_arg')
  })
})

describe('describe (schema introspection)', () => {
  it('lists all command names when called with no arg', () => {
    cli.cmdDescribe([])
    const out = lastJson()
    expect(out.ok).toBe(true)
    expect(out.commands).toContain('plans')
    expect(out.commands).toContain('describe')
  })

  it('returns schema for plans', () => {
    cli.cmdDescribe(['plans'])
    const out = lastJson()
    expect(out.ok).toBe(true)
    expect(out.command).toBe('plans')
    expect(out.positional[0].name).toBe('provider')
    expect(out.positional[0].required).toBe(true)
    expect(out.outputKeys).toContain('plans')
    expect(out.exitCodes['0']).toBe('success')
  })

  it('returns schema for recommend with flag descriptions', () => {
    cli.cmdDescribe(['recommend'])
    const out = lastJson()
    expect(out.flags.find((f: any) => f.name === '--geo').description).toContain('required')
  })

  it('errors on unknown command', () => {
    expect(() => cli.cmdDescribe(['nonexistent'])).toThrow(ExitError)
    const err = JSON.parse(stderrLines.at(-1)!)
    expect(err.code).toBe('unknown_subcommand')
  })

  it('rejects injection in command name', () => {
    expect(() => cli.cmdDescribe(['plans?id=foo'])).toThrow(ExitError)
    expect(JSON.parse(stderrLines.at(-1)!).code).toBe('bad_arg')
  })
})

describe('main dispatch', () => {
  it('--help prints usage', () => {
    cli.main(['--help'])
    expect(stdoutLines.join('')).toContain('USAGE')
  })

  it('no args prints usage', () => {
    cli.main([])
    expect(stdoutLines.join('')).toContain('capstan')
  })

  it('--version prints package version', () => {
    cli.main(['--version'])
    expect(lastJson().name).toBe('capstan')
  })

  it('unknown subcommand errors with stable code', () => {
    expect(() => cli.main(['nonexistent'])).toThrow(ExitError)
    const err = JSON.parse(stderrLines.at(-1)!)
    expect(err.code).toBe('unknown_subcommand')
  })

  it('routes "providers" to cmdProviders', () => {
    cli.main(['providers'])
    expect(lastJson().providers).toHaveLength(4)
  })
})
