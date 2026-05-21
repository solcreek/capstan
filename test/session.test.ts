import { describe, it, expect, vi } from 'vitest'
import { openEphemeralSession } from '../src/index.js'
import type {
  Account,
  Provider,
  ProviderName,
  ProvisionOptions,
  Region,
  Size,
  SSHKey,
  SSHKeyOptions,
  VPS,
} from '../src/index.js'

// Minimal in-memory provider for testing the session helper without
// touching the network. Records every call so tests can assert
// uploadSSHKey/createVPS/destroyVPS/deleteSSHKey were called in the
// correct sequence with the correct args.
function makeMockProvider(opts: {
  // Hook to inject failures at specific steps.
  uploadFails?: boolean
  createFails?: boolean
  destroyFails?: boolean
  deleteKeyFails?: boolean
  // If true, createVPS returns a VPS without publicIPv4 (forcing the
  // session to poll getVPS).
  noInitialIp?: boolean
  // Number of getVPS polls before publishing an IP. Used to simulate
  // "DO returns ipv4=null initially, publishes 30s later" behavior.
  // 0 = publish on first poll.
  getVPSPollsBeforeIP?: number
} = {}): Provider & { calls: string[] } {
  const calls: string[] = []
  let getVPSPollCount = 0

  const provider: Provider = {
    name: 'hetzner' as ProviderName,
    displayName: 'Mock',
    authenticate: async (): Promise<Account> => ({
      id: 'acct',
      name: 'mock',
    }),
    listSizes: async (): Promise<readonly Size[]> => [],
    listRegions: async (): Promise<readonly Region[]> => [],
    uploadSSHKey: async (kopts: SSHKeyOptions): Promise<SSHKey> => {
      calls.push(`uploadSSHKey(${kopts.name})`)
      if (opts.uploadFails) throw new Error('mock upload failed')
      return { id: 'k1', name: kopts.name, fingerprint: 'fp' }
    },
    listSSHKeys: async (): Promise<readonly SSHKey[]> => [],
    deleteSSHKey: async (id: string): Promise<void> => {
      calls.push(`deleteSSHKey(${id})`)
      if (opts.deleteKeyFails) throw new Error('mock delete key failed')
    },
    createVPS: async (vopts: ProvisionOptions): Promise<VPS> => {
      calls.push(`createVPS(${vopts.name})`)
      if (opts.createFails) throw new Error('mock create failed')
      return {
        id: 'v1',
        name: vopts.name,
        status: 'running',
        publicIPv4: opts.noInitialIp ? undefined : '203.0.113.10',
        region: vopts.region,
        size: vopts.size,
        createdAt: new Date().toISOString(),
      }
    },
    getVPS: async (id: string): Promise<VPS | null> => {
      calls.push(`getVPS(${id})`)
      const ready = getVPSPollCount >= (opts.getVPSPollsBeforeIP ?? 0)
      getVPSPollCount++
      return {
        id,
        name: 'v',
        status: 'running',
        publicIPv4: ready ? '203.0.113.11' : undefined,
        region: 'r',
        size: 's',
        createdAt: new Date().toISOString(),
      }
    },
    listVPS: async (): Promise<readonly VPS[]> => [],
    destroyVPS: async (id: string): Promise<void> => {
      calls.push(`destroyVPS(${id})`)
      if (opts.destroyFails) throw new Error('mock destroy failed')
    },
    estimateMonthlyCost: () => 0,
  }
  return Object.assign(provider, { calls })
}

describe('openEphemeralSession', () => {
  it('uploads the SSH key, creates the VPS, and tracks both for cleanup', async () => {
    const provider = makeMockProvider()
    const session = await openEphemeralSession(provider, {
      name: 'creek-test',
      size: 'cx23',
      region: 'fsn1',
      publicKey: 'ssh-ed25519 AAA...',
    })
    expect(provider.calls).toEqual([
      'uploadSSHKey(creek-test)',
      'createVPS(creek-test)',
    ])
    expect(session.vps.id).toBe('v1')
    expect(session.sshKey.id).toBe('k1')
  })

  it('forwards userData and labels through to createVPS', async () => {
    const provider = makeMockProvider()
    const createSpy = vi.spyOn(provider, 'createVPS')
    await openEphemeralSession(provider, {
      name: 'creek-test',
      size: 'cx23',
      region: 'fsn1',
      publicKey: 'ssh-ed25519 AAA...',
      userData: '#cloud-config\nruncmd:\n  - echo hi',
      labels: { purpose: 'bench' },
    })
    const passed = createSpy.mock.calls[0]![0]
    expect(passed.userData).toContain('#cloud-config')
    expect(passed.labels).toEqual({ purpose: 'bench' })
  })

  it('forwards image override through to createVPS when set', async () => {
    const provider = makeMockProvider()
    const createSpy = vi.spyOn(provider, 'createVPS')
    await openEphemeralSession(provider, {
      name: 'creek-test',
      size: 'cx23',
      region: 'fsn1',
      publicKey: 'ssh-ed25519 AAA...',
      image: 'debian-12',
    })
    const passed = createSpy.mock.calls[0]![0]
    expect(passed.image).toBe('debian-12')
  })

  it('omits image from createVPS when not set (provider default applies)', async () => {
    const provider = makeMockProvider()
    const createSpy = vi.spyOn(provider, 'createVPS')
    await openEphemeralSession(provider, {
      name: 'creek-test',
      size: 'cx23',
      region: 'fsn1',
      publicKey: 'ssh-ed25519 AAA...',
    })
    const passed = createSpy.mock.calls[0]![0]
    expect('image' in passed).toBe(false)
  })

  it('does not call createVPS if uploadSSHKey fails (no resource leak)', async () => {
    const provider = makeMockProvider({ uploadFails: true })
    await expect(
      openEphemeralSession(provider, {
        name: 'creek-test',
        size: 'cx23',
        region: 'fsn1',
        publicKey: 'ssh-ed25519 AAA...',
      }),
    ).rejects.toThrow(/mock upload failed/)
    expect(provider.calls).toEqual(['uploadSSHKey(creek-test)'])
  })

  it('rolls back the SSH key if createVPS fails', async () => {
    const provider = makeMockProvider({ createFails: true })
    await expect(
      openEphemeralSession(provider, {
        name: 'creek-test',
        size: 'cx23',
        region: 'fsn1',
        publicKey: 'ssh-ed25519 AAA...',
      }),
    ).rejects.toThrow(/mock create failed/)
    // The SSH key must be deleted after createVPS fails — otherwise the
    // caller would leak the key on every failed provision attempt.
    expect(provider.calls).toEqual([
      'uploadSSHKey(creek-test)',
      'createVPS(creek-test)',
      'deleteSSHKey(k1)',
    ])
  })

  it('surfaces the original createVPS error even if SSH key rollback also fails', async () => {
    const provider = makeMockProvider({
      createFails: true,
      deleteKeyFails: true,
    })
    // The createVPS error is the one operators need to see — the
    // rollback error is secondary and gets swallowed.
    await expect(
      openEphemeralSession(provider, {
        name: 'creek-test',
        size: 'cx23',
        region: 'fsn1',
        publicKey: 'ssh-ed25519 AAA...',
      }),
    ).rejects.toThrow(/mock create failed/)
  })

  describe('publicIP()', () => {
    it('returns immediately when createVPS already gave an IP', async () => {
      const provider = makeMockProvider()
      const session = await openEphemeralSession(provider, {
        name: 'creek-test',
        size: 'cx23',
        region: 'fsn1',
        publicKey: 'pk',
      })
      const ip = await session.publicIP()
      expect(ip).toBe('203.0.113.10')
      // No getVPS poll needed.
      expect(provider.calls.filter((c) => c.startsWith('getVPS'))).toEqual([])
    })

    it('polls getVPS until an IP is published', async () => {
      const provider = makeMockProvider({
        noInitialIp: true,
        getVPSPollsBeforeIP: 3, // ready on the 4th poll (index 3)
      })
      const session = await openEphemeralSession(provider, {
        name: 'creek-test',
        size: 'cx23',
        region: 'fsn1',
        publicKey: 'pk',
        waitForIP: { intervalMs: 1, timeoutMs: 1000 },
      })
      const ip = await session.publicIP()
      expect(ip).toBe('203.0.113.11')
      const getCalls = provider.calls.filter((c) => c.startsWith('getVPS'))
      expect(getCalls.length).toBe(4)
    })

    it('caches the IP across multiple calls — does not re-poll', async () => {
      const provider = makeMockProvider()
      const session = await openEphemeralSession(provider, {
        name: 'creek-test',
        size: 'cx23',
        region: 'fsn1',
        publicKey: 'pk',
      })
      await session.publicIP()
      await session.publicIP()
      await session.publicIP()
      expect(provider.calls.filter((c) => c.startsWith('getVPS'))).toEqual([])
    })

    it('throws on timeout if the IP is never published', async () => {
      const provider = makeMockProvider({
        noInitialIp: true,
        getVPSPollsBeforeIP: 1_000, // never ready within test budget
      })
      const session = await openEphemeralSession(provider, {
        name: 'creek-test',
        size: 'cx23',
        region: 'fsn1',
        publicKey: 'pk',
        waitForIP: { intervalMs: 1, timeoutMs: 20 },
      })
      await expect(session.publicIP()).rejects.toThrow(/did not publish/)
    })

    it('throws immediately when waitForIP is false and the VPS has no IP', async () => {
      const provider = makeMockProvider({ noInitialIp: true })
      const session = await openEphemeralSession(provider, {
        name: 'creek-test',
        size: 'cx23',
        region: 'fsn1',
        publicKey: 'pk',
        waitForIP: false,
      })
      await expect(session.publicIP()).rejects.toThrow(/waitForIP is disabled/)
    })
  })

  describe('dispose() + Symbol.asyncDispose', () => {
    it('destroys VPS first, then deletes SSH key', async () => {
      const provider = makeMockProvider()
      const session = await openEphemeralSession(provider, {
        name: 'creek-test',
        size: 'cx23',
        region: 'fsn1',
        publicKey: 'pk',
      })
      await session.dispose()
      const tail = provider.calls.slice(-2)
      expect(tail).toEqual(['destroyVPS(v1)', 'deleteSSHKey(k1)'])
    })

    it('is idempotent — multiple dispose calls do work only once', async () => {
      const provider = makeMockProvider()
      const session = await openEphemeralSession(provider, {
        name: 'creek-test',
        size: 'cx23',
        region: 'fsn1',
        publicKey: 'pk',
      })
      await session.dispose()
      await session.dispose()
      await session.dispose()
      // Only one destroy + one delete should happen.
      expect(provider.calls.filter((c) => c.startsWith('destroyVPS'))).toEqual(['destroyVPS(v1)'])
      expect(provider.calls.filter((c) => c.startsWith('deleteSSHKey'))).toEqual(['deleteSSHKey(k1)'])
    })

    it('does not throw if destroyVPS fails (best-effort cleanup)', async () => {
      const provider = makeMockProvider({ destroyFails: true })
      const session = await openEphemeralSession(provider, {
        name: 'creek-test',
        size: 'cx23',
        region: 'fsn1',
        publicKey: 'pk',
      })
      // Dispose should not throw — the operator wants both attempts to
      // happen even if one fails. The dispose contract is "best effort
      // never throw," because a thrown dispose during try/finally would
      // mask the original error.
      await expect(session.dispose()).resolves.toBeUndefined()
      // SSH key delete still attempted after VPS destroy failed.
      expect(provider.calls).toContain('deleteSSHKey(k1)')
    })

    it('attempts SSH key delete even if VPS destroy fails', async () => {
      const provider = makeMockProvider({ destroyFails: true })
      const session = await openEphemeralSession(provider, {
        name: 'creek-test',
        size: 'cx23',
        region: 'fsn1',
        publicKey: 'pk',
      })
      await session.dispose()
      // Both cleanups attempted, in order, despite the first failing.
      const cleanupCalls = provider.calls.filter(
        (c) => c.startsWith('destroyVPS') || c.startsWith('deleteSSHKey'),
      )
      expect(cleanupCalls).toEqual(['destroyVPS(v1)', 'deleteSSHKey(k1)'])
    })

    it('Symbol.asyncDispose is wired to dispose()', async () => {
      const provider = makeMockProvider()
      const session = await openEphemeralSession(provider, {
        name: 'creek-test',
        size: 'cx23',
        region: 'fsn1',
        publicKey: 'pk',
      })
      await session[Symbol.asyncDispose]()
      expect(provider.calls.slice(-2)).toEqual([
        'destroyVPS(v1)',
        'deleteSSHKey(k1)',
      ])
    })

    it('works under `await using` syntax', async () => {
      const provider = makeMockProvider()
      {
        await using session = await openEphemeralSession(provider, {
          name: 'creek-test',
          size: 'cx23',
          region: 'fsn1',
          publicKey: 'pk',
        })
        expect(session.vps.id).toBe('v1')
        // session disposes when the block exits below
      }
      // After scope exit, both resources cleaned up.
      expect(provider.calls.slice(-2)).toEqual([
        'destroyVPS(v1)',
        'deleteSSHKey(k1)',
      ])
    })
  })
})
