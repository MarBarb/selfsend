export interface DesktopBootstrap {
  configured: boolean
  base_url?: string
  autostart_enabled: boolean
}

export interface ServerIdentity {
  instance_id: string
  instance_name: string
  canonical_url: string
  state: 'active' | 'migrating' | 'retired'
  successor_url?: string
  server_device_name?: string
  deployment_type: 'local' | 'cloud' | 'nas'
  provider?: string
}

export interface Device {
  id: string
  name: string
  avatar: string
  created_at: number
  last_seen_at: number
  is_server: boolean
}

export interface InstanceStatus {
  setup_required: boolean
  authenticated: boolean
  max_upload_size: number
  version: string
  device?: Device
  server?: ServerIdentity
}

export interface Conversation {
  id: string
  conversation_id: string
  kind: 'direct' | 'group'
  name: string
  avatar: string
  created_at: number
  last_message_at: number
  last_kind?: 'file' | 'text'
  last_preview?: string
  member_count?: number
  is_server: boolean
}

interface TimelineBase {
  id: string
  kind: 'text' | 'file'
  created_at: number
  sender_device_id: string
  sender_name: string
  sender_avatar: string
}

export interface TextItem extends TimelineBase {
  kind: 'text'
  text: string
}

export interface FileItem extends TimelineBase {
  kind: 'file'
  file_name: string
  mime_type: string
  size: number
  sha256: string
  last_modified?: number
}

export type TimelineItem = TextItem | FileItem

export interface TimelineResponse {
  items: TimelineItem[]
  next_cursor: number
}

export interface UploadProgress {
  file_name: string
  fraction: number
}
