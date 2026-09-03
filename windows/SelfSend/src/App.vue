<script setup lang="ts">
import { computed, nextTick, onBeforeUnmount, onMounted, ref } from 'vue'
import { listen, type UnlistenFn } from '@tauri-apps/api/event'
import { invoke } from '@tauri-apps/api/core'
import { open, save } from '@tauri-apps/plugin-dialog'
import { api, desktop, errorMessage } from './api'
import type { Conversation, FileItem, InstanceStatus, TimelineItem, UploadProgress } from './types'

type Screen = 'loading' | 'connect' | 'auth' | 'pair' | 'chats' | 'chat' | 'settings' | 'moved'

const screen = ref<Screen>('loading')
const serverAddress = ref('')
const pairingToken = ref('')
const password = ref('')
const confirmation = ref('')
const deviceName = ref('Windows 电脑')
const status = ref<InstanceStatus>()
const conversations = ref<Conversation[]>([])
const activeConversation = ref<Conversation>()
const timeline = ref<TimelineItem[]>([])
const nextCursor = ref(0)
const draft = ref('')
const busy = ref(false)
const loadingOlder = ref(false)
const error = ref('')
const upload = ref<UploadProgress>()
const autostart = ref(true)
const timelineElement = ref<HTMLElement>()
let eventUnlisten: UnlistenFn | undefined
let uploadUnlisten: UnlistenFn | undefined

const title = computed(() => activeConversation.value?.name || status.value?.server?.instance_name || 'SelfSend')
const currentDeviceID = computed(() => status.value?.device?.id || '')
const authTitle = computed(() => status.value?.setup_required ? '初始化服务器' : '登录服务器')
const displayTimeline = computed(() => [...timeline.value].reverse())

onMounted(async () => {
  eventUnlisten = await listen<{ kind: string }>('server-event', ({ payload }) => {
    if (payload.kind === 'server-moved') void bootstrap()
    else if (screen.value === 'chat') void loadTimeline(true)
    else if (screen.value === 'chats') void loadConversations()
  })
  uploadUnlisten = await listen<UploadProgress>('upload-progress', ({ payload }) => { upload.value = payload })
  await bootstrap()
})

onBeforeUnmount(() => {
  eventUnlisten?.()
  uploadUnlisten?.()
})

async function bootstrap() {
  screen.value = 'loading'
  error.value = ''
  try {
    const value = await desktop.bootstrap()
    autostart.value = value.autostart_enabled
    if (!value.configured) {
      screen.value = 'connect'
      return
    }
    serverAddress.value = value.base_url || ''
    await routeFromStatus(await api.status())
  } catch (cause) {
    error.value = errorMessage(cause)
    screen.value = 'connect'
  }
}

async function connect() {
  await run(async () => {
    const parsed = parseConnection(serverAddress.value)
    serverAddress.value = await desktop.configureServer(parsed.address)
    pairingToken.value = parsed.token
    const loaded = await api.status()
    status.value = loaded
    if (parsed.token) screen.value = 'pair'
    else await routeFromStatus(loaded)
  })
}

async function routeFromStatus(loaded: InstanceStatus) {
  status.value = loaded
  if (loaded.server?.state === 'retired') {
    screen.value = 'moved'
    return
  }
  if (loaded.setup_required || !loaded.authenticated) {
    screen.value = 'auth'
    return
  }
  await enterApp(loaded)
}

async function authenticate() {
  if (password.value.length < 10) return void (error.value = '密码至少需要 10 个字符')
  if (status.value?.setup_required && password.value !== confirmation.value) return void (error.value = '两次输入的密码不一致')
  await run(async () => {
    if (status.value?.setup_required) await api.setup(password.value)
    else await api.login(password.value)
    password.value = ''
    confirmation.value = ''
    await enterApp(await api.status())
  })
}

async function pairDevice() {
  if (!deviceName.value.trim()) return void (error.value = '请输入设备名称')
  await run(async () => {
    await invoke('api_request', {
      request: {
        method: 'POST',
        path: '/api/pairing/claim',
        body: { token: pairingToken.value, name: deviceName.value.trim(), avatar: '🖥️' },
      },
    })
    pairingToken.value = ''
    await enterApp(await api.status())
  })
}

async function enterApp(loaded: InstanceStatus) {
  status.value = loaded
  if (!loaded.device) {
    const storageKey = `selfsend-device-id|${serverAddress.value}`
    const deviceID = localStorage.getItem(storageKey) || crypto.randomUUID()
    localStorage.setItem(storageKey, deviceID)
    await api.registerDevice({ id: deviceID, name: deviceName.value.trim() || 'Windows 电脑', avatar: '🖥️' })
    status.value = await api.status()
  }
  await loadConversations()
  screen.value = 'chats'
}

async function loadConversations() {
  try { conversations.value = await api.conversations() }
  catch (cause) { error.value = errorMessage(cause) }
}

async function openConversation(conversation: Conversation) {
  activeConversation.value = conversation
  timeline.value = []
  nextCursor.value = 0
  screen.value = 'chat'
  await loadTimeline(true)
}

async function loadTimeline(reset: boolean) {
  const conversation = activeConversation.value
  if (!conversation) return
  try {
    const page = await api.timeline(conversation.conversation_id, reset ? undefined : nextCursor.value)
    timeline.value = reset ? page.items : [...timeline.value, ...page.items]
    nextCursor.value = page.next_cursor
    if (reset) await scrollToBottom()
  } catch (cause) { error.value = errorMessage(cause) }
}

async function loadOlder() {
  if (!nextCursor.value || loadingOlder.value) return
  loadingOlder.value = true
  await loadTimeline(false)
  loadingOlder.value = false
}

async function sendText() {
  const text = draft.value.trim()
  const conversation = activeConversation.value
  if (!text || !conversation) return
  draft.value = ''
  try {
    await api.sendText(conversation.conversation_id, text)
    await loadTimeline(true)
  } catch (cause) {
    draft.value = text
    error.value = errorMessage(cause)
  }
}

async function chooseUpload() {
  const conversation = activeConversation.value
  if (!conversation) return
  const selected = await open({ multiple: true, directory: false })
  const paths = typeof selected === 'string' ? [selected] : selected || []
  if (!paths.length) return
  upload.value = { file_name: paths[0].split(/[\\/]/).pop() || '文件', fraction: 0 }
  await run(async () => {
    await desktop.uploadFiles(paths, conversation.conversation_id)
    await loadTimeline(true)
  })
  upload.value = undefined
}

async function download(item: FileItem) {
  const destination = await save({ defaultPath: item.file_name })
  if (!destination) return
  await run(() => desktop.downloadFile(item.id, destination))
}

async function deleteItem(item: TimelineItem) {
  if (item.sender_device_id !== currentDeviceID.value) return
  if (!confirm(`永久删除${item.kind === 'file' ? `“${item.file_name}”` : '这条消息'}？`)) return
  await run(async () => {
    await api.deleteItem(item.id)
    await loadTimeline(true)
  })
}

async function disconnect() {
  if (!confirm('从这台电脑移除当前服务器？服务器上的数据不会被删除。')) return
  await run(async () => {
    try { await api.logout() } catch { /* Clear the local session even when the server is offline. */ }
    await desktop.clearServer()
    status.value = undefined
    conversations.value = []
    activeConversation.value = undefined
    serverAddress.value = ''
    screen.value = 'connect'
  })
}

async function toggleAutostart() {
  const next = !autostart.value
  try {
    await desktop.setAutostart(next)
    autostart.value = next
  } catch (cause) { error.value = errorMessage(cause) }
}

async function run(action: () => Promise<void>) {
  busy.value = true
  error.value = ''
  try { await action() }
  catch (cause) { error.value = errorMessage(cause) }
  finally { busy.value = false }
}

function parseConnection(raw: string) {
  let value = raw.trim()
  if (!value.includes('://')) value = `http://${value}`
  const url = new URL(value)
  const token = new URLSearchParams(url.hash.replace(/^#/, '')).get('pair') || ''
  url.hash = ''
  url.search = ''
  url.pathname = ''
  return { address: url.toString(), token }
}

function preview(conversation: Conversation) {
  if (!conversation.last_message_at) return conversation.kind === 'group' ? `${conversation.member_count || 0} 台设备` : '现在可以发送消息或文件'
  return conversation.last_kind === 'file' ? `[文件] ${conversation.last_preview || ''}` : conversation.last_preview || ''
}

function formatTime(value: number) {
  if (!value) return ''
  const date = new Date(value)
  const today = new Date()
  return date.toDateString() === today.toDateString()
    ? date.toLocaleTimeString('zh-CN', { hour: '2-digit', minute: '2-digit' })
    : date.toLocaleDateString('zh-CN', { month: 'numeric', day: 'numeric' })
}

function formatSize(value: number) {
  if (value < 1024) return `${value} B`
  if (value < 1024 ** 2) return `${(value / 1024).toFixed(1)} KB`
  if (value < 1024 ** 3) return `${(value / 1024 ** 2).toFixed(1)} MB`
  return `${(value / 1024 ** 3).toFixed(1)} GB`
}

function imageAvatar(value?: string) {
  return Boolean(value?.startsWith('data:image/'))
}

async function scrollToBottom() {
  await nextTick()
  if (timelineElement.value) timelineElement.value.scrollTop = timelineElement.value.scrollHeight
}
</script>

<template>
  <main class="app-shell">
    <section v-if="screen === 'loading'" class="center-card splash">
      <div class="brand-mark">S</div>
      <h1>SelfSend</h1>
      <p>正在连接你的设备…</p>
    </section>

    <section v-else-if="screen === 'connect'" class="center-card">
      <div class="brand-mark">S</div>
      <h1>连接 SelfSend</h1>
      <p>输入服务器地址，或者粘贴另一台设备生成的邀请链接。</p>
      <form @submit.prevent="connect">
        <label>服务器地址或邀请链接</label>
        <input v-model="serverAddress" autofocus placeholder="http://192.168.1.20:8080" />
        <button class="primary" :disabled="busy || !serverAddress.trim()">连接</button>
      </form>
    </section>

    <section v-else-if="screen === 'auth'" class="center-card">
      <button class="back-link" @click="desktop.clearServer().then(bootstrap)">← 更换服务器</button>
      <div class="brand-mark small">S</div>
      <h1>{{ authTitle }}</h1>
      <p>{{ status?.setup_required ? '为这台新服务器设置管理员密码。' : `登录 ${status?.server?.instance_name || serverAddress}` }}</p>
      <form @submit.prevent="authenticate">
        <label>管理员密码</label>
        <input v-model="password" type="password" autocomplete="current-password" autofocus />
        <template v-if="status?.setup_required">
          <label>确认密码</label>
          <input v-model="confirmation" type="password" autocomplete="new-password" />
        </template>
        <button class="primary" :disabled="busy">{{ status?.setup_required ? '初始化并进入' : '登录' }}</button>
      </form>
    </section>

    <section v-else-if="screen === 'pair'" class="center-card">
      <button class="back-link" @click="screen = 'connect'">← 返回</button>
      <div class="device-mark">🖥️</div>
      <h1>添加这台电脑</h1>
      <p>邀请验证成功后，这台电脑会出现在其他设备的消息列表中。</p>
      <form @submit.prevent="pairDevice">
        <label>设备名称</label>
        <input v-model="deviceName" autofocus maxlength="40" />
        <button class="primary" :disabled="busy">加入 SelfSend</button>
      </form>
    </section>

    <section v-else-if="screen === 'moved'" class="center-card">
      <div class="device-mark">↗</div>
      <h1>服务器已经迁移</h1>
      <p>请连接迁移后的新地址。</p>
      <button class="primary" @click="desktop.clearServer().then(bootstrap)">重新连接</button>
    </section>

    <template v-else-if="screen === 'chats'">
      <header class="topbar">
        <div>
          <h1>{{ title }}</h1>
          <span><i /> 已连接</span>
        </div>
        <button class="round-button" title="设置" @click="screen = 'settings'">⚙</button>
      </header>
      <section class="conversation-list">
        <div v-if="!conversations.length" class="empty-state">
          <div>⇄</div>
          <h2>还没有其他设备</h2>
          <p>在已有设备中生成邀请链接，把手机或另一台电脑加入进来。</p>
        </div>
        <button v-for="conversation in conversations" :key="conversation.id" class="conversation" @click="openConversation(conversation)">
          <span class="avatar"><img v-if="imageAvatar(conversation.avatar)" :src="conversation.avatar" alt="" /><template v-else>{{ conversation.avatar || (conversation.is_server ? 'S' : '设备') }}</template></span>
          <span class="conversation-copy">
            <strong>{{ conversation.name }}</strong>
            <small>{{ preview(conversation) }}</small>
          </span>
          <time>{{ formatTime(conversation.last_message_at) }}</time>
        </button>
      </section>
    </template>

    <template v-else-if="screen === 'chat' && activeConversation">
      <header class="chatbar">
        <button class="round-button" @click="screen = 'chats'; activeConversation = undefined; loadConversations()">←</button>
        <div><h1>{{ activeConversation.name }}</h1><span>{{ activeConversation.kind === 'group' ? `${activeConversation.member_count || 0} 台设备` : 'SelfSend 设备' }}</span></div>
      </header>
      <section ref="timelineElement" class="timeline">
        <button v-if="nextCursor" class="older" :disabled="loadingOlder" @click="loadOlder">{{ loadingOlder ? '加载中…' : '查看更早消息' }}</button>
        <article v-for="item in displayTimeline" :key="item.id" class="message-row" :class="{ mine: item.sender_device_id === currentDeviceID }">
          <span class="message-avatar"><img v-if="imageAvatar(item.sender_avatar)" :src="item.sender_avatar" alt="" /><template v-else>{{ item.sender_avatar || '设备' }}</template></span>
          <div class="message-column">
            <small>{{ item.sender_name }}</small>
            <div v-if="item.kind === 'text'" class="bubble" @contextmenu.prevent="deleteItem(item)">{{ item.text }}</div>
            <button v-else class="file-card" @click="download(item)" @contextmenu.prevent="deleteItem(item)">
              <span class="file-icon">↓</span>
              <span><strong>{{ item.file_name }}</strong><small>{{ formatSize(item.size) }}</small></span>
            </button>
            <time>{{ formatTime(item.created_at) }}</time>
          </div>
        </article>
      </section>
      <div v-if="upload" class="upload-progress">
        <span>{{ upload.file_name }}</span><progress :value="upload.fraction" max="1" />
      </div>
      <form class="composer" @submit.prevent="sendText">
        <button type="button" class="attach" title="发送文件" :disabled="busy" @click="chooseUpload">＋</button>
        <textarea v-model="draft" rows="1" placeholder="输入消息" @keydown.enter.exact.prevent="sendText" />
        <button class="send" :disabled="!draft.trim()">发送</button>
      </form>
    </template>

    <template v-else-if="screen === 'settings'">
      <header class="chatbar">
        <button class="round-button" @click="screen = 'chats'">←</button>
        <div><h1>设置</h1><span>Windows 客户端</span></div>
      </header>
      <section class="settings-page">
        <div class="profile">
          <span class="avatar large"><img v-if="imageAvatar(status?.device?.avatar)" :src="status?.device?.avatar" alt="" /><template v-else>{{ status?.device?.avatar || '🖥️' }}</template></span>
          <div><strong>{{ status?.device?.name }}</strong><small>{{ serverAddress }}</small></div>
        </div>
        <div class="settings-group">
          <button @click="toggleAutostart">
            <span><strong>登录后自动启动</strong><small>仅启动托盘与消息连接，不预先打开窗口</small></span>
            <i class="switch" :class="{ on: autostart }"><b /></i>
          </button>
          <div><span><strong>关闭窗口时</strong><small>SelfSend 继续在系统托盘运行</small></span><em>保持运行</em></div>
        </div>
        <button class="danger" :disabled="busy" @click="disconnect">移除当前服务器</button>
        <p class="version">SelfSend Windows 0.1.0 · 服务器 {{ status?.version }}</p>
      </section>
    </template>

    <div v-if="error" class="toast" @click="error = ''">{{ error }}</div>
  </main>
</template>
