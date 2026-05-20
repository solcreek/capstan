# capstan

> Multi-provider VPS lifecycle library — Hetzner, DigitalOcean, Linode, Vultr behind one TypeScript interface.

**Status: 🚧 0.0.1 placeholder.** Real release coming. The provider abstraction currently lives in [groundflare](https://github.com/solcreek/groundflare/tree/main/packages/groundflare/src/provider); this repo is the upcoming extraction.

## What this will be

A small, focused library with one job: spin up and tear down VPSes across the four most common indie-friendly cloud providers (Hetzner, DigitalOcean, Linode, Vultr) behind a single `Provider` interface. Stateless, typed, MIT-licensed.

Designed to be the substrate that downstream tools (deployment tools, benchmark harnesses, multi-cloud orchestrators) build on instead of re-implementing per-provider HTTP clients.

## Why it's separate from groundflare

The provider/cloud-init abstraction was built inside [groundflare](https://github.com/solcreek/groundflare) ("Your Cloudflare Worker, grounded") but is conceptually independent of Workers. Extracting it makes it usable for benchmark labs, primitive orchestrators, and any tool that needs "give me a VPS to do stuff on, then clean up."

## Capstan?

A capstan is the rotating drum on a ship used to hoist heavy things — anchors, sails, cables. We're hoisting servers up and down. The metaphor lands.

## Reservation note

The npm name `capstan` and this GitHub repo are reserved as of 2026-05-20 ahead of the actual extraction. If you've reached this README looking for working code: it ships in 0.1.0, watch this repo.

## License

MIT. See LICENSE.
