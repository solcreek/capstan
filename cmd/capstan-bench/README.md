# capstan-bench

A small harness that measures provider API latency for the operations
that downstream consumers (dew, Marina-via-dew) call on every refresh.

## What it answers

- Is subprocess-per-call to capstan acceptable for Marina v0.1, or do we
  need a long-lived sidecar from day one?
- How long after `Create` does the new server appear in `List`? (provider
  eventual consistency)
- How long does `PowerOff → WaitForAction` take end-to-end?
- Does fanout (parallel `Get` for N servers) improve list-detail latency
  vs serial, or does the provider rate-limit it away?

## Usage

Read-only suite (free, just hits provider APIs):

```bash
HCLOUD_TOKEN=xxx go run ./cmd/capstan-bench \
  --provider hetzner --iterations 30
```

Include mutations (creates one billable server-hour):

```bash
HCLOUD_TOKEN=xxx go run ./cmd/capstan-bench \
  --provider hetzner --include-mutations \
  --server-type cax11 --region fsn1
```

Dump per-call NDJSON for post-hoc analysis:

```bash
HCLOUD_TOKEN=xxx go run ./cmd/capstan-bench \
  --provider hetzner --raw bench-$(date +%s).ndjson
```

## Flags

| Flag | Default | Notes |
|---|---|---|
| `--provider` | `hetzner` | One of `hetzner`, `digitalocean`, `linode`, `vultr` |
| `--iterations` | `30` | Iterations per read operation |
| `--concurrency` | `1,5,10` | Comma-separated concurrency levels for `Get` fanout |
| `--include-mutations` | `false` | Bench `Create` / `Power*` / `Destroy` (costs real money) |
| `--server-type` | `cax11` | Server plan for mutation test (cheapest Hetzner ARM by default) |
| `--region` | `fsn1` | Region for mutation test |
| `--raw` | `""` | If set, write NDJSON per measurement to this path |
| `--timeout` | `600` | Overall bench timeout in seconds |

## Tokens

The harness checks multiple env var names per provider, in priority order;
the first non-empty value wins. Aliases cover each vendor's official CLI,
Terraform's provider, and common shorthand:

| Provider | Aliases (priority order) |
|---|---|
| Hetzner | `HCLOUD_TOKEN`, `HETZNER_API_TOKEN`, `HETZNER_TOKEN` |
| DigitalOcean | `DIGITALOCEAN_TOKEN`, `DIGITALOCEAN_ACCESS_TOKEN`, `DOCTL_ACCESS_TOKEN`, `DO_TOKEN` |
| Linode | `LINODE_TOKEN`, `LINODE_CLI_TOKEN` |
| Vultr | `VULTR_API_KEY`, `VULTR_TOKEN` |

If none of the aliases are set, the harness exits with code 2 and prints
the full alias list in the error message.

## Cost note

The default mutation-test server (`cax11` at `fsn1`) bills at about
€0.006 / hour at the time of writing. A typical end-to-end run takes
≤ 5 minutes, so the cost is fractions of a euro. The harness always
destroys the test server at the end (deferred, runs even on bench
failure). If a run dies hard, look for `capstan-bench-<unix-ts>` and
delete it manually.

## Output

Markdown summary to stdout (operation × percentile table). Optional
NDJSON per-measurement to `--raw` for richer analysis (per-iteration
latency, error messages, timestamps).
