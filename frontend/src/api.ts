export interface InstanceStatus {
  setup_required: boolean
  authenticated: boolean
  max_upload_size: number
  version: string
  item_count?: number
  total_bytes?: number
  device?: Device
  server?: ServerIdentity
}

export interface ServerIdentity {
  instance_id: string
  instance_name: string
  canonical_url: string
  state: 'active' | 'migrating' | 'retired'
  successor_url?: string
  server_device_id?: string
  server_device_name?: string
  migration_epoch: number
}

export interface ServerDetails {
  server: ServerIdentity
  item_count: number
  total_bytes: number
  pending_uploads: number
  version: string
}

export interface MigrationJob {
  id?: string
  state: 'idle' | 'running' | 'completed' | 'failed'
  stage?: string
  target_url?: string
  target_name?: string
  total_bytes?: number
  sent_bytes?: number
  files?: number
  error?: string
}

export interface MigrationReceiver {
  id: string
  state: 'waiting' | 'uploading' | 'restarting' | 'applying' | 'active' | 'error'
  base_url: string
  host_name: string
  expires_at: number
  expected_size?: number
  offset?: number
  error?: string
}

export interface DiscoveredServer {
  instance_id: string
  name: string
  state: string
  url: string
  receiver: boolean
}

export interface FileItem {
  kind: 'file'
  id: string
  file_name: string
  mime_type: string
  size: number
  sha256: string
  created_at: number
  last_modified?: number
  sender_device_id: string
  sender_name: string
  sender_avatar: string
}

export interface TextItem {
  kind: 'text'
  id: string
  text: string
  created_at: number
  sender_device_id: string
  sender_name: string
  sender_avatar: string
}

export type TimelineItem = FileItem | TextItem

export interface Device {
  id: string
  name: string
  avatar: string
  created_at: number
  last_seen_at: number
  is_server: boolean
}

export interface Conversation {
  id: string
  conversation_id: string
  kind: 'direct' | 'group'
  name: string
  avatar: string
  created_at: number
  last_seen_at?: number
  last_message_at: number
  last_kind?: 'file' | 'text'
  last_preview?: string
  member_count?: number
  is_server: boolean
}

export interface PairingInvite {
  token: string
  expires_at: number
}

interface TimelineResponse {
  items: TimelineItem[]
  next_cursor: number
}

export class ApiError extends Error {
  constructor(public status: number, message: string) {
    super(message)
  }
}

async function request<T>(url: string, init?: RequestInit): Promise<T> {
  const response = await fetch(url, {
    credentials: 'same-origin',
    ...init,
    headers: {
      ...(init?.body ? { 'Content-Type': 'application/json' } : {}),
      ...init?.headers,
    },
  })
  if (!response.ok) {
    let message = `请求失败 (${response.status})`
    try {
      const body = await response.json() as { error?: string }
      if (body.error) message = body.error
    } catch {
      // Keep the status-based message when the server did not return JSON.
    }
    throw new ApiError(response.status, message)
  }
  if (response.status === 204) return undefined as T
  return response.json() as Promise<T>
}

export const api = {
  status: () => request<InstanceStatus>('/api/status'),
  setup: (password: string) => request<{ ok: boolean }>('/api/setup', {
    method: 'POST', body: JSON.stringify({ password }),
  }),
  login: (password: string) => request<{ ok: boolean }>('/api/login', {
    method: 'POST', body: JSON.stringify({ password }),
  }),
  logout: () => request<void>('/api/logout', { method: 'POST' }),
  registerDevice: (device: Pick<Device, 'id' | 'name' | 'avatar'>) => request<Device>('/api/devices/register', {
    method: 'POST', body: JSON.stringify(device),
  }),
  conversations: () => request<{ conversations: Conversation[] }>('/api/conversations'),
	createGroup: (deviceIDs: string[]) => request<Conversation>('/api/groups', {
		method: 'POST', body: JSON.stringify({ device_ids: deviceIDs }),
	}),
  createPairingInvite: () => request<PairingInvite>('/api/pairing/invites', { method: 'POST' }),
  claimPairing: (token: string, device: Pick<Device, 'name' | 'avatar'>) => request<{ device: Device }>('/api/pairing/claim', {
    method: 'POST', body: JSON.stringify({ token, ...device }),
  }),
  updateDevice: (id: string, value: Pick<Device, 'name' | 'avatar'>) => request<Device>(`/api/devices/${id}`, {
    method: 'PATCH', body: JSON.stringify(value),
  }),
  timeline: (conversationID: string, before?: number) => request<TimelineResponse>(`/api/items?conversation_id=${encodeURIComponent(conversationID)}&limit=50${before ? `&before=${before}` : ''}`),
  sendText: (conversationID: string, text: string) => request<TextItem>('/api/notes', {
    method: 'POST', body: JSON.stringify({ conversation_id: conversationID, text }),
  }),
  deleteItem: (id: string) => request<void>(`/api/items/${id}`, { method: 'DELETE' }),
  server: () => request<ServerDetails>('/api/server'),
  discoverServers: () => request<{ servers: DiscoveredServer[] }>('/api/server/discovery'),
  startMigration: (targetURL: string, token: string, password: string, mode: 'local' | 'online' = 'local') => request<MigrationJob>('/api/server/migrations', {
    method: 'POST', body: JSON.stringify({ target_url: targetURL, token, password, mode }),
  }),
  migrationStatus: () => request<MigrationJob>('/api/server/migrations/current'),
  rollbackMigration: (password: string) => request<{ ok: boolean }>('/api/server/migrations/rollback', { method: 'POST', body: JSON.stringify({ password }) }),
  createHandoff: () => request<{ url: string }>('/api/server/handoff', { method: 'POST' }),
  claimHandoff: (token: string) => request<{ ok: boolean }>('/api/migration/handoff', { method: 'POST', body: JSON.stringify({ token }) }),
  createReceiver: (name: string, avatar: string) => request<{ id: string; token: string; offer_url: string; expires_at: number }>('/api/migration/receivers', {
    method: 'POST', body: JSON.stringify({ name, avatar }),
  }),
  receiverStatus: (token: string) => receiverRequest<MigrationReceiver>('/api/migration/receivers/current', token),
  claimReceiver: (token: string) => request<{ ok: boolean }>('/api/migration/receiver/claim', { method: 'POST', body: JSON.stringify({ token }) }),
}

async function receiverRequest<T>(url: string, token: string, init?: RequestInit): Promise<T> {
  const response = await fetch(url, { ...init, headers: { Authorization: `Bearer ${token}`, ...init?.headers } })
  if (!response.ok) throw new ApiError(response.status, await responseMessage(response))
  if (response.status === 204) return undefined as T
  return response.json() as Promise<T>
}

async function responseMessage(response: Response) {
  try { return ((await response.json()) as { error?: string }).error || `请求失败 (${response.status})` }
  catch { return `请求失败 (${response.status})` }
}

export const prepareBackup = (password: string) => request<{ download_url: string }>('/api/server/backups', { method: 'POST', body: JSON.stringify({ password }) })

export async function restoreBackup(file: File, token: string, onProgress: (progress: number) => void) {
  const initialize = await receiverRequest<{ offset: number }>('/api/migration/receivers/current/archive', token, {
    method: 'POST', headers: { 'Content-Type': 'application/json' }, body: JSON.stringify({ size: file.size, sha256: '', instance_id: '' }),
  })
  let offset = initialize.offset
  while (offset < file.size) {
    const chunk = file.slice(offset, Math.min(offset + chunkSize, file.size))
    const response = await fetch('/api/migration/receivers/current/archive', {
      method: 'PATCH', headers: { Authorization: `Bearer ${token}`, 'Content-Type': 'application/offset+octet-stream', 'Upload-Offset': String(offset) }, body: chunk,
    })
    if (!response.ok) throw new ApiError(response.status, await responseMessage(response))
    offset = Number(response.headers.get('Upload-Offset') || offset + chunk.size)
    onProgress(file.size ? offset / file.size : 1)
  }
  await receiverRequest('/api/migration/receivers/current/activate', token, { method: 'POST' })
}

const chunkSize = 4 * 1024 * 1024

export async function uploadFile(
  file: File,
  conversationID: string,
  onProgress: (progress: number) => void,
  signal?: AbortSignal,
): Promise<void> {
  const fingerprint = `selfsend:${conversationID}:${file.name}:${file.size}:${file.lastModified}`
  let location = localStorage.getItem(fingerprint) || ''
  let offset = 0

  if (location.startsWith('/api/uploads/')) {
    const head = await fetch(location, { method: 'HEAD', credentials: 'same-origin', signal })
    if (head.ok) {
      offset = Number(head.headers.get('Upload-Offset') || '0')
      if (!Number.isFinite(offset) || offset < 0 || offset > file.size) offset = 0
    } else {
      location = ''
      localStorage.removeItem(fingerprint)
    }
  }

  if (!location) {
    const metadata = [
      `filename ${utf8Base64(file.name)}`,
      `filetype ${utf8Base64(file.type || 'application/octet-stream')}`,
      `lastmodified ${utf8Base64(String(file.lastModified))}`,
      `conversation ${utf8Base64(conversationID)}`,
    ].join(',')
    const response = await fetch('/api/uploads', {
      method: 'POST',
      credentials: 'same-origin',
      signal,
      headers: {
        'Tus-Resumable': '1.0.0',
        'Upload-Length': String(file.size),
        'Upload-Metadata': metadata,
      },
    })
    if (!response.ok) throw await uploadError(response)
    location = response.headers.get('Location') || ''
    if (!location.startsWith('/api/uploads/')) throw new Error('服务器返回了无效的上传地址')
    localStorage.setItem(fingerprint, location)
  }

  onProgress(file.size === 0 ? 1 : offset / file.size)
  while (offset < file.size) {
    const chunk = file.slice(offset, Math.min(offset + chunkSize, file.size))
    const response = await fetch(location, {
      method: 'PATCH',
      credentials: 'same-origin',
      signal,
      headers: {
        'Content-Type': 'application/offset+octet-stream',
        'Tus-Resumable': '1.0.0',
        'Upload-Offset': String(offset),
      },
      body: chunk,
    })
    if (!response.ok) throw await uploadError(response)
    offset = Number(response.headers.get('Upload-Offset') || offset + chunk.size)
    onProgress(offset / file.size)
  }
  localStorage.removeItem(fingerprint)
  onProgress(1)
}

async function uploadError(response: Response): Promise<ApiError> {
  let message = `上传失败 (${response.status})`
  try {
    const body = await response.json() as { error?: string }
    if (body.error) message = body.error
  } catch {
    // Keep the status-based message.
  }
  return new ApiError(response.status, message)
}

function utf8Base64(value: string): string {
  const bytes = new TextEncoder().encode(value)
  let binary = ''
  bytes.forEach((byte) => { binary += String.fromCharCode(byte) })
  return btoa(binary)
}
