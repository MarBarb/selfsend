import { invoke } from '@tauri-apps/api/core'
import type {
  Conversation,
  DesktopBootstrap,
  Device,
  InstanceStatus,
  TextItem,
  TimelineResponse,
} from './types'

interface NativeRequest {
  method: string
  path: string
  body?: unknown
}

async function request<T>(method: string, path: string, body?: unknown): Promise<T> {
  return invoke<T>('api_request', { request: { method, path, body } satisfies NativeRequest })
}

export const desktop = {
  bootstrap: () => invoke<DesktopBootstrap>('desktop_bootstrap'),
  configureServer: (address: string) => invoke<string>('configure_server', { address }),
  clearServer: () => invoke<void>('clear_server'),
  autostartEnabled: () => invoke<boolean>('autostart_enabled'),
  setAutostart: (enabled: boolean) => invoke<void>('set_autostart', { enabled }),
  uploadFiles: (paths: string[], conversationID: string) =>
    invoke<void>('upload_files', { paths, conversationId: conversationID }),
  downloadFile: (itemID: string, destination: string) =>
    invoke<void>('download_file', { itemId: itemID, destination }),
}

export const api = {
  status: () => request<InstanceStatus>('GET', '/api/status'),
  setup: (password: string) => request<{ ok: boolean }>('POST', '/api/setup', { password }),
  login: (password: string) => request<{ ok: boolean }>('POST', '/api/login', { password }),
  logout: () => request<void>('POST', '/api/logout'),
  registerDevice: (device: Pick<Device, 'id' | 'name' | 'avatar'>) =>
    request<Device>('POST', '/api/devices/register', device),
  conversations: async () =>
    (await request<{ conversations: Conversation[] }>('GET', '/api/conversations')).conversations,
  timeline: (conversationID: string, before?: number) =>
    request<TimelineResponse>(
      'GET',
      `/api/items?conversation_id=${encodeURIComponent(conversationID)}&limit=50${before ? `&before=${before}` : ''}`,
    ),
  sendText: (conversationID: string, text: string) =>
    request<TextItem>('POST', '/api/notes', { conversation_id: conversationID, text }),
  deleteItem: (id: string) => request<void>('DELETE', `/api/items/${encodeURIComponent(id)}`),
}

export function errorMessage(error: unknown): string {
  if (typeof error === 'string') return error
  if (error instanceof Error) return error.message
  return '操作失败，请稍后重试'
}
