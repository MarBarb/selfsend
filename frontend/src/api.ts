export interface InstanceStatus {
  setup_required: boolean
  authenticated: boolean
  max_upload_size: number
  version: string
  item_count?: number
  total_bytes?: number
}

export interface FileItem {
  id: string
  file_name: string
  mime_type: string
  size: number
  sha256: string
  created_at: number
  last_modified?: number
}

interface TimelineResponse {
  items: FileItem[]
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
  timeline: (before?: number) => request<TimelineResponse>(`/api/items?limit=50${before ? `&before=${before}` : ''}`),
  deleteItem: (id: string) => request<void>(`/api/items/${id}`, { method: 'DELETE' }),
}

const chunkSize = 4 * 1024 * 1024

export async function uploadFile(
  file: File,
  onProgress: (progress: number) => void,
  signal?: AbortSignal,
): Promise<void> {
  const fingerprint = `selfsend:${file.name}:${file.size}:${file.lastModified}`
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
