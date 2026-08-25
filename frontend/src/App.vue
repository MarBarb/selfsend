<script setup lang="ts">
import { computed, nextTick, onBeforeUnmount, onMounted, ref } from 'vue'
import { api, ApiError, type FileItem, type InstanceStatus, uploadFile } from './api'

type ViewState = 'loading' | 'setup' | 'login' | 'timeline' | 'error'
type UploadState = 'queued' | 'uploading' | 'done' | 'error'

interface UploadTask {
  id: string
  file: File
  progress: number
  state: UploadState
  error?: string
}

const view = ref<ViewState>('loading')
const status = ref<InstanceStatus | null>(null)
const password = ref('')
const passwordConfirm = ref('')
const formError = ref('')
const submitting = ref(false)
const items = ref<FileItem[]>([])
const nextCursor = ref(0)
const loadingTimeline = ref(false)
const uploads = ref<UploadTask[]>([])
const dragging = ref(false)
const fileInput = ref<HTMLInputElement | null>(null)
const timeline = ref<HTMLElement | null>(null)
let events: EventSource | null = null
let activeWorkers = 0

const totalLabel = computed(() => {
  if (!status.value?.authenticated) return ''
  return `${status.value.item_count || 0} 个文件 · ${formatBytes(status.value.total_bytes || 0)}`
})

onMounted(bootstrap)
onBeforeUnmount(() => events?.close())

async function bootstrap() {
  try {
    status.value = await api.status()
    if (status.value.setup_required) view.value = 'setup'
    else if (!status.value.authenticated) view.value = 'login'
    else await enterTimeline()
  } catch {
    view.value = 'error'
  }
}

async function submitSetup() {
  formError.value = ''
  if (password.value.length < 10) {
    formError.value = '密码至少需要 10 个字符'
    return
  }
  if (password.value !== passwordConfirm.value) {
    formError.value = '两次输入的密码不一致'
    return
  }
  submitting.value = true
  try {
    await api.setup(password.value)
    password.value = ''
    passwordConfirm.value = ''
    status.value = await api.status()
    await enterTimeline()
  } catch (error) {
    formError.value = error instanceof Error ? error.message : '初始化失败'
  } finally {
    submitting.value = false
  }
}

async function submitLogin() {
  formError.value = ''
  submitting.value = true
  try {
    await api.login(password.value)
    password.value = ''
    status.value = await api.status()
    await enterTimeline()
  } catch (error) {
    formError.value = error instanceof Error ? error.message : '登录失败'
  } finally {
    submitting.value = false
  }
}

async function enterTimeline() {
  view.value = 'timeline'
  await loadTimeline(true)
  connectEvents()
}

async function logout() {
  events?.close()
  await api.logout()
  items.value = []
  view.value = 'login'
}

async function loadTimeline(reset = false) {
  if (loadingTimeline.value) return
  loadingTimeline.value = true
  try {
    const response = await api.timeline(reset ? undefined : nextCursor.value || undefined)
    items.value = reset ? response.items : [...items.value, ...response.items]
    nextCursor.value = response.next_cursor
    status.value = await api.status()
    if (reset) await scrollToLatest()
  } catch (error) {
    if (error instanceof ApiError && error.status === 401) view.value = 'login'
  } finally {
    loadingTimeline.value = false
  }
}

function connectEvents() {
  events?.close()
  events = new EventSource('/api/events')
  events.addEventListener('timeline', () => loadTimeline(true))
}

function chooseFiles() {
  fileInput.value?.click()
}

function selected(event: Event) {
  const target = event.target as HTMLInputElement
  if (target.files) enqueue(Array.from(target.files))
  target.value = ''
}

function dropped(event: DragEvent) {
  dragging.value = false
  if (event.dataTransfer?.files) enqueue(Array.from(event.dataTransfer.files))
}

function enqueue(files: File[]) {
  const max = status.value?.max_upload_size || Number.MAX_SAFE_INTEGER
  files.forEach((file) => {
    if (file.size > max) {
      uploads.value.unshift({ id: taskID(), file, progress: 0, state: 'error', error: `超过 ${formatBytes(max)} 限制` })
    } else {
      uploads.value.unshift({ id: taskID(), file, progress: 0, state: 'queued' })
    }
  })
  runQueue()
}

function runQueue() {
  while (activeWorkers < 2) {
    const task = uploads.value.find((item) => item.state === 'queued')
    if (!task) return
    activeWorkers++
    task.state = 'uploading'
    uploadFile(task.file, (progress) => { task.progress = progress })
      .then(async () => {
        task.state = 'done'
        await loadTimeline(true)
        window.setTimeout(() => {
          uploads.value = uploads.value.filter((item) => item.id !== task.id)
        }, 2500)
      })
      .catch((error) => {
        task.state = 'error'
        task.error = error instanceof Error ? error.message : '上传失败'
      })
      .finally(() => {
        activeWorkers--
        runQueue()
      })
  }
}

function retry(task: UploadTask) {
  task.state = 'queued'
  task.error = undefined
  runQueue()
}

async function removeItem(item: FileItem) {
  if (!window.confirm(`永久删除“${item.file_name}”？`)) return
  try {
    await api.deleteItem(item.id)
    items.value = items.value.filter((candidate) => candidate.id !== item.id)
    status.value = await api.status()
  } catch (error) {
    window.alert(error instanceof Error ? error.message : '删除失败')
  }
}

function scrollToLatest() {
  return nextTick(() => {
    if (timeline.value) timeline.value.scrollTop = timeline.value.scrollHeight
  })
}

function formatBytes(bytes: number): string {
  if (!bytes) return '0 B'
  const units = ['B', 'KB', 'MB', 'GB', 'TB']
  const index = Math.min(Math.floor(Math.log(bytes) / Math.log(1024)), units.length - 1)
  const value = bytes / Math.pow(1024, index)
  return `${value >= 10 || index === 0 ? value.toFixed(0) : value.toFixed(1)} ${units[index]}`
}

function formatTime(timestamp: number): string {
  return new Intl.DateTimeFormat('zh-CN', { hour: '2-digit', minute: '2-digit' }).format(new Date(timestamp))
}

function formatDay(timestamp: number): string {
  const date = new Date(timestamp)
  const today = new Date()
  if (date.toDateString() === today.toDateString()) return '今天'
  const yesterday = new Date(today)
  yesterday.setDate(today.getDate() - 1)
  if (date.toDateString() === yesterday.toDateString()) return '昨天'
  return new Intl.DateTimeFormat('zh-CN', { month: 'long', day: 'numeric', weekday: 'short' }).format(date)
}

function showDay(index: number): boolean {
  if (index === items.value.length - 1) return true
  const current = new Date(items.value[index].created_at).toDateString()
  const older = new Date(items.value[index + 1].created_at).toDateString()
  return current !== older
}

function iconFor(item: FileItem): string {
  if (item.mime_type.startsWith('image/')) return 'IMG'
  if (item.mime_type.startsWith('video/')) return 'VID'
  if (item.mime_type.startsWith('audio/')) return 'AUD'
  if (item.mime_type.includes('pdf')) return 'PDF'
  if (item.mime_type.includes('zip') || item.mime_type.includes('compressed')) return 'ZIP'
  return 'FILE'
}

function canPreview(item: FileItem): boolean {
  return item.mime_type.startsWith('image/') && item.size <= 15 * 1024 * 1024
}

function taskID(): string {
  return globalThis.crypto?.randomUUID?.() || `${Date.now()}-${Math.random().toString(36).slice(2)}`
}
</script>

<template>
  <main v-if="view === 'loading'" class="center-screen">
    <div class="brand-mark loading-mark">S</div>
    <p class="muted">正在连接 SelfSend…</p>
  </main>

  <main v-else-if="view === 'setup' || view === 'login'" class="center-screen auth-background">
    <section class="auth-card">
      <div class="brand-mark">S</div>
      <h1>SelfSend</h1>
      <p class="tagline">像聊天一样，给自己发文件。</p>
      <form v-if="view === 'setup'" @submit.prevent="submitSetup">
        <div class="welcome-note">
          <strong>欢迎来到你的私人文件空间</strong>
          <span>请创建管理员密码。SelfSend 没有账号系统，也不会连接任何官方云服务。</span>
        </div>
        <label>
          管理员密码
          <input v-model="password" type="password" autocomplete="new-password" minlength="10" autofocus placeholder="至少 10 个字符" />
        </label>
        <label>
          再次输入
          <input v-model="passwordConfirm" type="password" autocomplete="new-password" minlength="10" placeholder="确认密码" />
        </label>
        <p v-if="formError" class="form-error">{{ formError }}</p>
        <button class="primary-button" :disabled="submitting">{{ submitting ? '正在创建…' : '创建私人空间' }}</button>
      </form>
      <form v-else @submit.prevent="submitLogin">
        <label>
          管理员密码
          <input v-model="password" type="password" autocomplete="current-password" autofocus placeholder="输入密码" />
        </label>
        <p v-if="formError" class="form-error">{{ formError }}</p>
        <button class="primary-button" :disabled="submitting">{{ submitting ? '正在进入…' : '进入 SelfSend' }}</button>
      </form>
      <p class="privacy-line">数据只保存在这台 SelfSend 服务器上</p>
    </section>
  </main>

  <main v-else-if="view === 'error'" class="center-screen">
    <div class="brand-mark error-mark">!</div>
    <h1>无法连接服务器</h1>
    <p class="muted">请确认 SelfSend 服务仍在运行，然后重试。</p>
    <button class="secondary-button" @click="bootstrap">重新连接</button>
  </main>

  <main
    v-else
    class="app-shell"
    :class="{ dragging }"
    @dragenter.prevent="dragging = true"
    @dragover.prevent="dragging = true"
    @dragleave.self.prevent="dragging = false"
    @drop.prevent="dropped"
  >
    <header class="topbar">
      <div class="topbar-brand">
        <div class="small-mark">S</div>
        <div>
          <h1>SelfSend</h1>
          <p>{{ totalLabel }}</p>
        </div>
      </div>
      <button class="icon-button logout-button" title="退出登录" @click="logout">
        <svg viewBox="0 0 24 24" aria-hidden="true"><path d="M10 17l5-5-5-5M15 12H3m9-9h7a2 2 0 012 2v14a2 2 0 01-2 2h-7" /></svg>
      </button>
    </header>

    <section ref="timeline" class="timeline">
      <button v-if="nextCursor" class="load-more" :disabled="loadingTimeline" @click="loadTimeline(false)">
        {{ loadingTimeline ? '载入中…' : '查看更早的文件' }}
      </button>

      <div v-if="!items.length && !uploads.length" class="empty-state">
        <div class="empty-illustration">
          <span class="empty-file">FILE</span>
          <span class="empty-arrow">↑</span>
        </div>
        <h2>发点什么给自己</h2>
        <p>图片、文档、压缩包，或者任何你想在另一台设备上取走的文件。</p>
        <button class="primary-button compact" @click="chooseFiles">选择文件</button>
      </div>

      <div class="messages">
        <template v-for="(item, index) in items" :key="item.id">
          <div v-if="showDay(index)" class="day-separator">{{ formatDay(item.created_at) }}</div>
          <article class="message-row">
            <div class="message-content">
              <div class="message-time">{{ formatTime(item.created_at) }}</div>
              <div class="file-bubble">
                <a v-if="canPreview(item)" class="image-preview" :href="`/api/files/${item.id}`" download>
                  <img :src="`/api/files/${item.id}?inline=1`" :alt="item.file_name" loading="lazy" />
                </a>
                <div class="file-card">
                  <div class="file-type" :class="iconFor(item).toLowerCase()">{{ iconFor(item) }}</div>
                  <div class="file-details">
                    <strong :title="item.file_name">{{ item.file_name }}</strong>
                    <span>{{ formatBytes(item.size) }}</span>
                  </div>
                  <a class="download-button" :href="`/api/files/${item.id}`" download title="下载文件">
                    <svg viewBox="0 0 24 24" aria-hidden="true"><path d="M12 3v12m0 0l-4-4m4 4l4-4M5 20h14" /></svg>
                  </a>
                </div>
                <button class="delete-button" @click="removeItem(item)">删除</button>
              </div>
            </div>
            <div class="avatar">我</div>
          </article>
        </template>
      </div>
    </section>

    <section v-if="uploads.length" class="upload-panel">
      <article v-for="task in uploads" :key="task.id" class="upload-task">
        <div class="upload-copy">
          <strong>{{ task.file.name }}</strong>
          <span v-if="task.state === 'queued'">等待上传</span>
          <span v-else-if="task.state === 'uploading'">{{ Math.round(task.progress * 100) }}% · 请保持页面打开</span>
          <span v-else-if="task.state === 'done'" class="success-text">上传完成</span>
          <span v-else class="error-text">{{ task.error }}</span>
        </div>
        <button v-if="task.state === 'error'" class="retry-button" @click="retry(task)">重试</button>
        <div v-if="task.state === 'uploading'" class="progress-track"><i :style="{ width: `${task.progress * 100}%` }"></i></div>
      </article>
    </section>

    <footer class="composer">
      <button class="attach-button" @click="chooseFiles">
        <svg viewBox="0 0 24 24" aria-hidden="true"><path d="M21.4 11.6l-9.2 9.2a6 6 0 01-8.5-8.5l9.2-9.2a4 4 0 015.7 5.7l-9.2 9.2a2 2 0 01-2.8-2.8l8.5-8.5" /></svg>
        <span>选择文件</span>
      </button>
      <p>大文件传输期间，请保持此页面打开</p>
      <input ref="fileInput" class="visually-hidden" type="file" multiple @change="selected" />
    </footer>

    <div v-if="dragging" class="drop-overlay">
      <div><strong>松开发给自己</strong><span>文件将保存到你的 SelfSend 服务器</span></div>
    </div>
  </main>
</template>
