// Ephemeral session helper.
//
// Every lab script / bench tool that uses capstan repeats the same
// ~30 lines of resource-tracking boilerplate:
//
//   - upload SSH key, remember to delete it later
//   - create VPS, remember to destroy it later
//   - poll for publicIPv4 if the provider didn't return one
//   - wire up SIGINT + try/finally to guarantee cleanup
//
// `openEphemeralSession()` collapses that to ~5 lines using the
// `await using` pattern (Node 22+, TS 5.2+ — both already required
// by capstan):
//
//   await using session = await openEphemeralSession(provider, {
//     name: 'creek-bench',
//     size: 'cx43',
//     region: 'fsn1',
//     publicKey: pubKey,
//     userData: cloudInit,
//   })
//   const ip = await session.publicIP()
//   // ... do work over ssh to `ip` ...
//   // session disposes automatically: destroyVPS + deleteSSHKey
//
// For callers that can't use `await using` (older codebases, REPL
// experiments), `session.dispose()` does the same thing imperatively.

import type { Provider, SSHKey, VPS } from './types.js'

export interface EphemeralSessionOptions {
  /**
   * Display name applied to both the SSH key and the VPS. Convention:
   * include a timestamp suffix so concurrent runs don't collide
   * (e.g. `creek-bench-${Date.now()}`).
   */
  readonly name: string

  /** Provider-specific size id, e.g. `cx43` or `g6-nanode-1`. */
  readonly size: string

  /** Provider-specific region id, e.g. `fsn1` or `ap-northeast`. */
  readonly region: string

  /**
   * SSH public key (OpenSSH format) — caller generates the keypair
   * locally and provides the public half here. The private half stays
   * with the caller so they can SSH in once the VPS comes up.
   */
  readonly publicKey: string

  /** Optional cloud-init `#cloud-config` user-data. */
  readonly userData?: string

  /** Optional provider labels / tags. */
  readonly labels?: Record<string, string>

  /**
   * Poll behavior for `publicIP()`. Set to `false` to disable polling
   * entirely (publicIP() then rejects immediately if the VPS was
   * created without an IP). Default: poll every 2s for up to 60s.
   */
  readonly waitForIP?: { intervalMs?: number; timeoutMs?: number } | false
}

export interface EphemeralSession {
  /** The created VPS. Snapshot from createVPS() — does not auto-refresh. */
  readonly vps: VPS

  /** The uploaded SSH key, registered for auto-cleanup. */
  readonly sshKey: SSHKey

  /**
   * Returns the VPS's public IPv4. If the VPS came up with an IP
   * already, returns immediately. Otherwise polls `provider.getVPS()`
   * until an IP is published or the configured timeout elapses.
   *
   * Throws if the timeout is exceeded.
   *
   * Cached: subsequent calls return the same address without re-polling.
   */
  publicIP(): Promise<string>

  /**
   * Destroys the VPS and deletes the SSH key. Best-effort — never
   * throws; logs errors via the provided onError callback if either
   * resource is already gone or the API call fails. Idempotent: safe
   * to call multiple times, only first call does work.
   */
  dispose(): Promise<void>

  /**
   * Same as `dispose()`. Lets the session be used with `await using`:
   *
   *   await using session = await openEphemeralSession(provider, opts)
   *
   * Node 22+ and TypeScript 5.2+ required.
   */
  [Symbol.asyncDispose](): Promise<void>
}

const DEFAULT_WAIT_INTERVAL_MS = 2_000
const DEFAULT_WAIT_TIMEOUT_MS = 60_000

const sleep = (ms: number): Promise<void> =>
  new Promise((resolve) => setTimeout(resolve, ms))

/**
 * Open an ephemeral capstan session: upload an SSH key, provision a
 * VPS, and return a handle that auto-cleans both when disposed.
 *
 * Both `await using` and explicit `dispose()` are supported. See the
 * file-header comment for examples.
 *
 * Resource handling guarantees:
 *
 * - If `uploadSSHKey()` fails, no resources are leaked.
 * - If `uploadSSHKey()` succeeds but `createVPS()` fails, the SSH key
 *   is rolled back before the error propagates.
 * - If both succeed, the session is "live" until `dispose()` is called.
 *   `dispose()` destroys the VPS first (to release public IP) then
 *   deletes the SSH key, swallowing errors at each step so a partial
 *   cleanup is still attempted.
 */
export async function openEphemeralSession(
  provider: Provider,
  opts: EphemeralSessionOptions,
): Promise<EphemeralSession> {
  // Step 1: upload SSH key. If this fails, nothing is leaked.
  const sshKey = await provider.uploadSSHKey({
    name: opts.name,
    publicKey: opts.publicKey,
  })

  // Step 2: create VPS. If this fails, roll back the SSH key.
  let vps: VPS
  try {
    vps = await provider.createVPS({
      name: opts.name,
      size: opts.size,
      region: opts.region,
      sshKeyIds: [sshKey.id],
      ...(opts.userData !== undefined ? { userData: opts.userData } : {}),
      ...(opts.labels !== undefined ? { labels: opts.labels } : {}),
    })
  } catch (createErr) {
    // Best-effort SSH key rollback — swallow secondary error so
    // the original createVPS error surfaces.
    try {
      await provider.deleteSSHKey(sshKey.id)
    } catch {
      // Ignore: the createVPS error is more important.
    }
    throw createErr
  }

  return new EphemeralSessionImpl(provider, sshKey, vps, opts.waitForIP)
}

class EphemeralSessionImpl implements EphemeralSession {
  readonly vps: VPS
  readonly sshKey: SSHKey

  private cachedIP: string | undefined
  private disposed = false
  private readonly waitOpts: EphemeralSessionOptions['waitForIP']

  constructor(
    private readonly provider: Provider,
    sshKey: SSHKey,
    vps: VPS,
    waitOpts: EphemeralSessionOptions['waitForIP'],
  ) {
    this.sshKey = sshKey
    this.vps = vps
    this.cachedIP = vps.publicIPv4 ?? undefined
    this.waitOpts = waitOpts
  }

  async publicIP(): Promise<string> {
    if (this.cachedIP !== undefined) return this.cachedIP

    if (this.waitOpts === false) {
      throw new Error(
        `VPS ${this.vps.id} has no public IPv4 and waitForIP is disabled`,
      )
    }

    const intervalMs =
      this.waitOpts?.intervalMs ?? DEFAULT_WAIT_INTERVAL_MS
    const timeoutMs = this.waitOpts?.timeoutMs ?? DEFAULT_WAIT_TIMEOUT_MS
    const deadline = Date.now() + timeoutMs

    // Poll getVPS until an IP is published. Some providers (DO) return
    // ipv4=null from createVPS and only publish the address ~10-30s later.
    while (Date.now() < deadline) {
      const refreshed = await this.provider.getVPS(this.vps.id)
      if (refreshed?.publicIPv4) {
        this.cachedIP = refreshed.publicIPv4
        return this.cachedIP
      }
      await sleep(intervalMs)
    }

    throw new Error(
      `VPS ${this.vps.id} did not publish a public IPv4 within ${timeoutMs}ms`,
    )
  }

  async dispose(): Promise<void> {
    if (this.disposed) return
    this.disposed = true

    // Best-effort: destroy VPS first (releases IP), then delete key.
    // Each step swallows its own error so a failure in one doesn't
    // skip the other.
    try {
      await this.provider.destroyVPS(this.vps.id)
    } catch {
      // VPS may already be gone (404), or a transient transport
      // error — either way, continue to key cleanup.
    }
    try {
      await this.provider.deleteSSHKey(this.sshKey.id)
    } catch {
      // SSH key may already be gone or shared with another resource
      // (unlikely with ephemeral pattern) — best effort.
    }
  }

  [Symbol.asyncDispose](): Promise<void> {
    return this.dispose()
  }
}
