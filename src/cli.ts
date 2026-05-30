#!/usr/bin/env node
// capstan — CLI for quick provider/plan/region/placement lookup.
//
// Design notes:
//   - Output is JSON by default (agent-first). Use --text for human
//     line-oriented output. Errors are always JSON on stderr.
//   - Offline commands read from embedded specs/*.json — no token, no
//     network, sub-millisecond response time.
//   - Live commands hit provider APIs and require a token via the
//     conventional env vars per provider.
//   - Exit codes: 0 success, 1 logic error (drift detected, unknown
//     plan, etc.), 2 user error (bad args, missing token, unknown
//     subcommand).

import { parseArgs, type ParseArgsConfig } from 'node:util'
import {
  recommendPlacement,
  type Geography,
  type SlaTier,
  type Workload,
} from './posture.js'
import { createProvider, listImplementedProviders } from './registry.js'
import type { ProviderName } from './types.js'

import hetznerSpec from '../specs/hetzner.json' with { type: 'json' }
import digitaloceanSpec from '../specs/digitalocean.json' with { type: 'json' }
import linodeSpec from '../specs/linode.json' with { type: 'json' }
import vultrSpec from '../specs/vultr.json' with { type: 'json' }
import postureSpec from '../specs/posture.json' with { type: 'json' }
import pkg from '../package.json' with { type: 'json' }

const SPECS = {
  hetzner: hetznerSpec,
  digitalocean: digitaloceanSpec,
  linode: linodeSpec,
  vultr: vultrSpec,
} as const

const PROVIDERS: readonly ProviderName[] = ['hetzner', 'digitalocean', 'linode', 'vultr']

const WORKLOADS: readonly Workload[] = ['io-multitenant', 'cpu-latency', 'cpu-throughput', 'general']
const SLA_TIERS: readonly SlaTier[] = ['best-effort', 'standard', 'premium']
const GEOGRAPHIES: readonly Geography[] = [
  'eu-central', 'eu-north', 'eu-west', 'eu-south',
  'us-east', 'us-central', 'us-west',
  'canada', 'singapore', 'japan', 'india',
  'indonesia', 'australia', 'latam',
]

const TOKEN_ALIASES: Record<ProviderName, readonly string[]> = {
  hetzner: ['HCLOUD_TOKEN', 'HETZNER_API_TOKEN', 'HETZNER_TOKEN'],
  digitalocean: [
    'DIGITALOCEAN_TOKEN', 'DIGITALOCEAN_ACCESS_TOKEN',
    'DOCTL_ACCESS_TOKEN', 'DO_API_KEY', 'DO_TOKEN',
  ],
  linode: ['LINODE_TOKEN', 'LINODE_CLI_TOKEN'],
  vultr: ['VULTR_API_KEY', 'VULTR_TOKEN'],
}

// ─── Output helpers ────────────────────────────────────────────────

interface EmitOpts {
  text?: boolean
}

export function emit<T>(
  data: T,
  textRenderer?: (data: T) => string,
  opts: EmitOpts = {},
): void {
  if (opts.text && textRenderer) {
    process.stdout.write(textRenderer(data) + '\n')
  } else {
    process.stdout.write(JSON.stringify(data, null, 2) + '\n')
  }
}

// Narrow parseArgs values (which are typed string|boolean|undefined under
// strict:false) into our EmitOpts. Keeps each command body a one-liner.
function optsOf(values: { text?: string | boolean }): EmitOpts {
  return { text: Boolean(values.text) }
}

export function emitError(message: string, code: string, exit = 1): never {
  process.stderr.write(JSON.stringify({ ok: false, error: message, code }, null, 2) + '\n')
  process.exit(exit)
}

// ─── Argument parsing helpers ──────────────────────────────────────

const commonOpts: ParseArgsConfig['options'] = {
  text: { type: 'boolean' },
  help: { type: 'boolean', short: 'h' },
}

export function assertProvider(name: string | undefined): asserts name is ProviderName {
  if (!name) {
    emitError('provider name required', 'missing_arg', 2)
  }
  if (!(PROVIDERS as readonly string[]).includes(name)) {
    emitError(
      `unknown provider: ${name}. supported: ${PROVIDERS.join(', ')}`,
      'unknown_provider',
      2,
    )
  }
}

// ─── Offline subcommands ───────────────────────────────────────────

export function cmdProviders(argv: string[]): void {
  const { values } = parseArgs({ args: argv, options: commonOpts, strict: false })
  emit(
    { ok: true, providers: [...PROVIDERS] },
    (d) => d.providers.join('\n'),
    optsOf(values),
  )
}

export function cmdWorkloads(argv: string[]): void {
  const { values } = parseArgs({ args: argv, options: commonOpts, strict: false })
  emit(
    { ok: true, workloads: [...WORKLOADS] },
    (d) => d.workloads.join('\n'),
    optsOf(values),
  )
}

export function cmdSlaTiers(argv: string[]): void {
  const { values } = parseArgs({ args: argv, options: commonOpts, strict: false })
  emit(
    { ok: true, slaTiers: [...SLA_TIERS] },
    (d) => d.slaTiers.join('\n'),
    optsOf(values),
  )
}

export function cmdGeographies(argv: string[]): void {
  const { values } = parseArgs({ args: argv, options: commonOpts, strict: false })
  emit(
    { ok: true, geographies: [...GEOGRAPHIES] },
    (d) => d.geographies.join('\n'),
    optsOf(values),
  )
}

export function cmdPlans(argv: string[]): void {
  const { values, positionals } = parseArgs({
    args: argv,
    options: commonOpts,
    allowPositionals: true,
    strict: false,
  })
  const providerName = positionals[0]
  assertProvider(providerName)

  const spec = SPECS[providerName]
  const plans = Object.entries(spec.priceCents)
    .map(([id, cents]) => ({ id, priceMonthlyCents: cents, priceCurrency: spec.priceCurrency }))
    .sort((a, b) => a.priceMonthlyCents - b.priceMonthlyCents)

  emit(
    { ok: true, provider: providerName, currency: spec.priceCurrency, plans },
    (d) => d.plans.map((p) => `${p.id}\t${p.priceMonthlyCents} ${d.currency}`).join('\n'),
    optsOf(values),
  )
}

export function cmdPrice(argv: string[]): void {
  const { values, positionals } = parseArgs({
    args: argv,
    options: commonOpts,
    allowPositionals: true,
    strict: false,
  })
  const providerName = positionals[0]
  const planId = positionals[1]
  assertProvider(providerName)
  if (!planId) emitError('plan id required', 'missing_arg', 2)

  const spec = SPECS[providerName]
  const cents = (spec.priceCents as Record<string, number>)[planId]
  if (cents === undefined) {
    emitError(
      `plan "${planId}" not found in ${providerName} spec; run \`capstan plans ${providerName}\` to list valid plans`,
      'unknown_plan',
      1,
    )
  }

  emit(
    {
      ok: true,
      provider: providerName,
      plan: planId,
      priceMonthlyCents: cents,
      priceCurrency: spec.priceCurrency,
    },
    (d) => `${d.priceMonthlyCents} ${d.priceCurrency} /mo`,
    optsOf(values),
  )
}

export function cmdRecommend(argv: string[]): void {
  const { values } = parseArgs({
    args: argv,
    options: {
      text: { type: 'boolean' },
      help: { type: 'boolean', short: 'h' },
      geo: { type: 'string' },
      workload: { type: 'string' },
      sla: { type: 'string' },
    },
    strict: false,
  })

  if (!values.geo) {
    emitError(
      'missing --geo flag. valid values: ' + GEOGRAPHIES.join(', '),
      'missing_arg',
      2,
    )
  }

  const geo = values.geo as string
  if (!(GEOGRAPHIES as readonly string[]).includes(geo)) {
    emitError(`unknown geography: ${geo}. supported: ${GEOGRAPHIES.join(', ')}`, 'bad_arg', 2)
  }

  try {
    const rec = recommendPlacement({
      geography: geo as Geography,
      workload: (values.workload as Workload) ?? 'general',
      sla: (values.sla as SlaTier) ?? 'standard',
    })
    emit(
      { ok: true, ...rec },
      (d) =>
        [
          `primary: ${d.primary.provider}/${d.primary.region}/${d.primary.size}`,
          ...(d.fallbacks.length
            ? ['fallbacks:', ...d.fallbacks.map((p) => `  ${p.provider}/${p.region}/${p.size}`)]
            : []),
          ...(d.caveats.length ? ['caveats:', ...d.caveats.map((c) => `  - ${c}`)] : []),
        ].join('\n'),
      optsOf(values),
    )
  } catch (err) {
    emitError(String(err instanceof Error ? err.message : err), 'recommend_failed', 1)
  }
}

// ─── Dispatch ──────────────────────────────────────────────────────

const SUBCOMMANDS: Record<string, (argv: string[]) => void> = {
  providers: cmdProviders,
  workloads: cmdWorkloads,
  'sla-tiers': cmdSlaTiers,
  geographies: cmdGeographies,
  plans: cmdPlans,
  price: cmdPrice,
  recommend: cmdRecommend,
}

const HELP = `capstan — multi-provider VPS lookup CLI

USAGE
  capstan <command> [args...] [--text]

OFFLINE COMMANDS (no token, instant)
  providers                                List supported providers
  workloads                                List workload classes
  sla-tiers                                List SLA tiers
  geographies                              List geographies
  plans <provider>                         List plans (size + price) from spec
  price <provider> <plan>                  Single plan monthly price (cents)
  recommend --geo <g> [--workload] [--sla] Placement recommendation

GLOBAL FLAGS
  --text          Human-readable output (default: JSON for agent use)
  -h, --help      Show this help
  -v, --version   Print version

Token aliases (when a future live command needs auth):
  hetzner       HCLOUD_TOKEN | HETZNER_API_TOKEN | HETZNER_TOKEN
  digitalocean  DIGITALOCEAN_TOKEN | DIGITALOCEAN_ACCESS_TOKEN |
                DOCTL_ACCESS_TOKEN | DO_API_KEY | DO_TOKEN
  linode        LINODE_TOKEN | LINODE_CLI_TOKEN
  vultr         VULTR_API_KEY | VULTR_TOKEN
`

export function main(argv: string[]): void {
  const [sub, ...rest] = argv

  if (!sub || sub === '--help' || sub === '-h') {
    process.stdout.write(HELP)
    return
  }
  if (sub === '--version' || sub === '-v') {
    emit(
      { ok: true, version: pkg.version, name: 'capstan' },
      (d) => d.version,
      { text: argv.includes('--text') },
    )
    return
  }

  const handler = SUBCOMMANDS[sub]
  if (!handler) {
    emitError(
      `unknown subcommand: ${sub}. run \`capstan --help\` for the list`,
      'unknown_subcommand',
      2,
    )
  }
  handler(rest)
}

// Reference touched so tsc doesn't strip the import in CLI-only builds.
void listImplementedProviders
void createProvider
void postureSpec

// Run main when invoked directly (not when imported by tests).
if (import.meta.url === `file://${process.argv[1]}`) {
  main(process.argv.slice(2))
}
