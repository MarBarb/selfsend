export type ServerDeployment = 'local' | 'cloud' | 'nas'

export interface SavedServer {
  instanceId: string
  name: string
  url: string
  deploymentType: ServerDeployment
  provider?: string
  customName?: boolean
  lastConnectedAt: number
}

const storageKey = 'selfsend-server-directory-v1'
const transportKey = 'servers'
const maximumServers = 24
const maximumTransportLength = 48 * 1024

export function loadServerDirectory(): SavedServer[] {
  try {
    const parsed = JSON.parse(localStorage.getItem(storageKey) || '[]') as unknown
    return validateDirectory(parsed)
  } catch {
    return []
  }
}

export function saveServerDirectory(servers: SavedServer[]): SavedServer[] {
  const validated = validateDirectory(servers)
  try { localStorage.setItem(storageKey, JSON.stringify(validated)) } catch { /* Private browsing may reject storage writes. */ }
  return validated
}

export function importTransportedServerDirectory(): SavedServer[] {
  const local = loadServerDirectory()
  const params = new URLSearchParams(window.location.hash.replace(/^#/, ''))
  const transported = params.get(transportKey)
  if (!transported || transported.length > maximumTransportLength) return local

  params.delete(transportKey)
  const suffix = params.toString() ? `#${params.toString()}` : ''
  window.history.replaceState(null, '', `${window.location.pathname}${window.location.search}${suffix}`)
  try {
    const decoded = decodeTransport(transported)
    return saveServerDirectory(mergeDirectories(local, validateDirectory(JSON.parse(decoded))))
  } catch {
    return local
  }
}

export function rememberCurrentServer(
  servers: SavedServer[],
  identity: { instance_id: string; server_device_name?: string; instance_name?: string; deployment_type: ServerDeployment; provider?: string },
): SavedServer[] {
  const url = normalizeServerURL(window.location.origin)
  const existing = servers.find((server) => server.instanceId === identity.instance_id || server.url === url)
  const profile: SavedServer = {
    instanceId: identity.instance_id,
    name: existing?.customName ? existing.name : identity.server_device_name || identity.instance_name || defaultServerName(identity.deployment_type),
    url,
    deploymentType: identity.deployment_type,
    provider: identity.provider,
    customName: existing?.customName,
    lastConnectedAt: Date.now(),
  }
  return saveServerDirectory(mergeDirectories(servers, [profile]))
}

export function createPendingServer(name: string, value: string, deploymentType: ServerDeployment): SavedServer {
  return {
    instanceId: `pending:${globalThis.crypto?.randomUUID?.() || `${Date.now()}-${Math.random().toString(36).slice(2)}`}`,
    name: name.trim() || defaultServerName(deploymentType),
    url: normalizeServerURL(value),
    deploymentType,
    customName: Boolean(name.trim()),
    lastConnectedAt: 0,
  }
}

export function serverNavigationURL(target: SavedServer, servers: SavedServer[]): string {
	return carryServerDirectory(normalizeServerURL(target.url), servers)
}

export function carryServerDirectory(value: string, servers: SavedServer[]): string {
	const url = new URL(value)
	const params = new URLSearchParams(url.hash.replace(/^#/, ''))
	params.set(transportKey, encodeTransport(JSON.stringify(validateDirectory(servers))))
	url.hash = params.toString()
	return url.toString()
}

export function normalizeServerURL(value: string): string {
  const url = new URL(value.trim())
  if (url.protocol !== 'http:' && url.protocol !== 'https:') throw new Error('服务器地址必须以 http:// 或 https:// 开头')
  if (url.username || url.password) throw new Error('服务器地址不能包含用户名或密码')
  if (url.protocol === 'http:' && isPublicHostname(url.hostname)) throw new Error('公网服务器必须使用 HTTPS 地址')
  return url.origin
}

export function defaultServerName(deploymentType: ServerDeployment): string {
  if (deploymentType === 'cloud') return '云端服务器'
  if (deploymentType === 'nas') return 'NAS 服务器'
  return '本地服务器'
}

function mergeDirectories(base: SavedServer[], incoming: SavedServer[]): SavedServer[] {
  const result = [...base]
  for (const candidate of incoming) {
    const index = result.findIndex((server) => server.instanceId === candidate.instanceId || server.url === candidate.url)
    if (index < 0) result.push(candidate)
    else {
      const current = result[index]
      result[index] = {
        ...current,
        ...candidate,
        name: current.customName && !candidate.customName ? current.name : candidate.name,
        customName: Boolean(current.customName || candidate.customName),
        lastConnectedAt: Math.max(current.lastConnectedAt, candidate.lastConnectedAt),
      }
    }
  }
  return result
    .filter((server, index, all) => all.findIndex((item) => item.instanceId === server.instanceId || item.url === server.url) === index)
    .sort((left, right) => right.lastConnectedAt - left.lastConnectedAt)
    .slice(0, maximumServers)
}

function validateDirectory(value: unknown): SavedServer[] {
  if (!Array.isArray(value)) return []
  const result: SavedServer[] = []
  for (const item of value.slice(0, maximumServers)) {
    if (!item || typeof item !== 'object') continue
    const candidate = item as Partial<SavedServer>
    if (typeof candidate.instanceId !== 'string' || !candidate.instanceId || candidate.instanceId.length > 100) continue
    if (typeof candidate.name !== 'string' || !candidate.name.trim() || candidate.name.length > 60) continue
    if (candidate.deploymentType !== 'local' && candidate.deploymentType !== 'cloud' && candidate.deploymentType !== 'nas') continue
    try {
      result.push({
        instanceId: candidate.instanceId,
        name: candidate.name.trim(),
        url: normalizeServerURL(String(candidate.url || '')),
        deploymentType: candidate.deploymentType,
        provider: typeof candidate.provider === 'string' ? candidate.provider.slice(0, 40) : undefined,
        customName: Boolean(candidate.customName),
        lastConnectedAt: Number.isFinite(candidate.lastConnectedAt) ? Number(candidate.lastConnectedAt) : 0,
      })
    } catch { /* Ignore malformed imported entries. */ }
  }
  return mergeValidated(result)
}

function mergeValidated(servers: SavedServer[]): SavedServer[] {
  const result: SavedServer[] = []
  for (const server of servers) {
    const index = result.findIndex((item) => item.instanceId === server.instanceId || item.url === server.url)
    if (index < 0) result.push(server)
    else if (server.lastConnectedAt >= result[index].lastConnectedAt) result[index] = server
  }
  return result.sort((left, right) => right.lastConnectedAt - left.lastConnectedAt).slice(0, maximumServers)
}

function isPublicHostname(hostname: string): boolean {
  const host = hostname.replace(/^\[|\]$/g, '').toLowerCase()
  if (host === 'localhost' || host.endsWith('.local') || host.endsWith('.home') || host.endsWith('.lan')) return false
  if (host === '::1' || host.startsWith('fe80:') || host.startsWith('fc') || host.startsWith('fd')) return false
  const octets = host.split('.').map(Number)
  if (octets.length === 4 && octets.every((part) => Number.isInteger(part) && part >= 0 && part <= 255)) {
    return !(octets[0] === 10 || octets[0] === 127 || (octets[0] === 169 && octets[1] === 254) || (octets[0] === 172 && octets[1] >= 16 && octets[1] <= 31) || (octets[0] === 192 && octets[1] === 168))
  }
  return true
}

function encodeTransport(value: string): string {
  const bytes = new TextEncoder().encode(value)
  let binary = ''
  bytes.forEach((byte) => { binary += String.fromCharCode(byte) })
  return btoa(binary).replace(/\+/g, '-').replace(/\//g, '_').replace(/=+$/g, '')
}

function decodeTransport(value: string): string {
  const base64 = value.replace(/-/g, '+').replace(/_/g, '/') + '='.repeat((4 - value.length % 4) % 4)
  const binary = atob(base64)
  const bytes = Uint8Array.from(binary, (character) => character.charCodeAt(0))
  return new TextDecoder().decode(bytes)
}
