<script setup lang="ts">
import { computed, nextTick, onBeforeUnmount, onMounted, ref } from 'vue'
import {
  ArrowDownTrayIcon,
  ChatBubbleOvalLeftEllipsisIcon,
  ChevronLeftIcon,
  ChevronRightIcon,
  DocumentIcon,
  PhotoIcon,
  PlusCircleIcon,
	UserPlusIcon,
	UserGroupIcon,
	DevicePhoneMobileIcon,
	ClipboardDocumentIcon,
	CheckIcon,
  ServerStackIcon,
  ComputerDesktopIcon,
  CloudIcon,
  ArrowPathIcon,
  ArchiveBoxArrowDownIcon,
  UserIcon,
  XMarkIcon,
} from '@heroicons/vue/24/outline'
import QrcodeVue from 'qrcode.vue'
import { api, ApiError, prepareBackup, restoreBackup, type Conversation, type Device, type DiscoveredServer, type FileItem, type InstanceStatus, type MigrationJob, type MigrationReceiver, type PairingInvite, type ServerDetails, type TimelineItem, uploadFile } from './api'

type ViewState = 'loading' | 'setup' | 'login' | 'pairing' | 'receiver' | 'moved' | 'app' | 'error'
type AppScreen = 'chats' | 'chat' | 'me' | 'server'
type UploadState = 'queued' | 'uploading' | 'done' | 'error'
type MigrationScreen = 'destination' | 'local-target' | 'transfer' | 'nas-guide' | 'online'
type LocalMigrationTarget = 'computer' | 'nas'

interface UploadTask {
  id: string
  conversationID: string
  file: File
  progress: number
  state: UploadState
  error?: string
}

const view = ref<ViewState>('loading')
const screen = ref<AppScreen>('chats')
const status = ref<InstanceStatus | null>(null)
const password = ref('')
const passwordConfirm = ref('')
const formError = ref('')
const submitting = ref(false)
const currentDeviceID = ref('')
const devices = ref<Conversation[]>([])
const activeDevice = ref<Conversation | null>(null)
const items = ref<TimelineItem[]>([])
const nextCursor = ref(0)
const loadingTimeline = ref(false)
const uploads = ref<UploadTask[]>([])
const dragging = ref(false)
const fileInput = ref<HTMLInputElement | null>(null)
const photoInput = ref<HTMLInputElement | null>(null)
const avatarInput = ref<HTMLInputElement | null>(null)
const composerInput = ref<HTMLTextAreaElement | null>(null)
const timeline = ref<HTMLElement | null>(null)
const messageText = ref('')
const plusOpen = ref(false)
const sendingText = ref(false)
const editorOpen = ref(false)
const editName = ref('')
const editAvatar = ref('')
const editSaving = ref(false)
const addFriendOpen = ref(false)
const invite = ref<PairingInvite | null>(null)
const inviteLoading = ref(false)
const inviteCopied = ref(false)
const pairingToken = ref('')
const pairingName = ref('')
const pairingAvatar = ref('')
const pairingSubmitting = ref(false)
const pairingDone = ref(false)
const serverOnline = ref(true)
const headerMenuOpen = ref(false)
const groupCreatorOpen = ref(false)
const selectedGroupDeviceIDs = ref<string[]>([])
const groupCreating = ref(false)
const wideLayout = ref(false)
const setupMode = ref<'choose' | 'create' | 'receive'>('choose')
const receiverName = ref('新服务器')
const receiverAvatar = ref('💻')
const receiverToken = ref('')
const receiverOfferURL = ref('')
const receiverStatus = ref<MigrationReceiver | null>(null)
const receiverProgress = ref(0)
const receiverUploading = ref(false)
const serverDetails = ref<ServerDetails | null>(null)
const serverLoading = ref(false)
const migrationOpen = ref(false)
const migrationScreen = ref<MigrationScreen>('destination')
const localMigrationTarget = ref<LocalMigrationTarget>('computer')
const migrationOffer = ref('')
const migrationPassword = ref('')
const migrationStarting = ref(false)
const migrationJob = ref<MigrationJob | null>(null)
const discoveredServers = ref<DiscoveredServer[]>([])
const discoveryLoading = ref(false)
const nasCommandCopied = ref(false)
const backupOpen = ref(false)
const backupPassword = ref('')
const backupCreating = ref(false)
const rollbackOpen = ref(false)
const rollbackPassword = ref('')
let events: EventSource | null = null
let activeWorkers = 0
let healthTimer = 0
let wideLayoutQuery: MediaQueryList | null = null
let migrationTimer = 0
let receiverTimer = 0

const avatarPresets = ['我', '📱', '💻', '🖥️', '📂', '🟢', '🐼', '🐱']
const totalLabel = computed(() => !status.value?.authenticated ? '' : `${status.value.item_count || 0} 个文件 · ${formatBytes(status.value.total_bytes || 0)}`)
const currentDevice = computed(() => status.value?.device || null)
const activeUploads = computed(() => uploads.value.filter((task) => task.conversationID === activeDevice.value?.conversation_id))
const displayItems = computed(() => [...items.value].reverse())
const inviteURL = computed(() => invite.value ? `${window.location.origin}/#pair=${encodeURIComponent(invite.value.token)}` : '')
const localOnlyOrigin = computed(() => ['localhost', '127.0.0.1', '[::1]'].includes(window.location.hostname))
const serverStatusLabel = computed(() => !serverOnline.value ? '本地服务器离线' : currentDevice.value?.is_server ? '这台设备是服务器' : '本地服务器在线')
const groupCandidates = computed(() => devices.value.filter((conversation) => conversation.kind === 'direct'))
const migrationTitle = computed(() => {
	if (migrationJob.value) return '正在更换服务器'
	if (migrationScreen.value === 'local-target') return '更换为本地服务器'
	if (migrationScreen.value === 'transfer') return localMigrationTarget.value === 'nas' ? '迁移到 NAS' : '迁移到电脑'
	if (migrationScreen.value === 'nas-guide') return '在 NAS 上启动 SelfSend'
	if (migrationScreen.value === 'online') return '更换为在线服务器'
	return '更换服务器'
})
const nasInstallCommand = `mkdir -p selfsend && cd selfsend
curl -fsSL https://raw.githubusercontent.com/MarBarb/selfsend/main/compose.nas.yaml -o compose.yaml
docker compose up -d`

onMounted(() => {
	wideLayoutQuery = window.matchMedia('(min-width: 900px)')
	wideLayout.value = wideLayoutQuery.matches
	wideLayoutQuery.addEventListener('change', updateWideLayout)
	void bootstrap()
	healthTimer = window.setInterval(checkServer, 10_000)
	window.addEventListener('online', checkServer)
	window.addEventListener('offline', markServerOffline)
})
onBeforeUnmount(() => {
	events?.close()
	window.clearInterval(healthTimer)
	window.removeEventListener('online', checkServer)
	window.removeEventListener('offline', markServerOffline)
	wideLayoutQuery?.removeEventListener('change', updateWideLayout)
	window.clearInterval(migrationTimer)
	window.clearInterval(receiverTimer)
})

function updateWideLayout(event: MediaQueryListEvent) { wideLayout.value = event.matches }

async function bootstrap() {
  try {
		const handoffToken = readHashToken('handoff')
		if (handoffToken) {
			await api.claimHandoff(handoffToken)
			clearHash()
		}
		const migrationReceiverToken = readHashToken('receive')
		if (migrationReceiverToken) {
			receiverToken.value = migrationReceiverToken
			try {
				await api.claimReceiver(migrationReceiverToken)
				clearHash()
			} catch {
				view.value = 'receiver'
				await refreshReceiverStatus()
				startReceiverPolling()
				return
			}
		}
    status.value = await api.status()
		serverOnline.value = true
		pairingToken.value = readPairingToken()
		if (pairingToken.value) {
			const defaults = status.value.device || detectDeviceDefaults()
			pairingName.value = defaults.name
			pairingAvatar.value = defaults.avatar
			view.value = 'pairing'
			return
		}
		if (status.value.server?.state === 'retired') { view.value = 'moved'; return }
		if (status.value.setup_required) { setupMode.value = 'choose'; view.value = 'setup' }
    else if (!status.value.authenticated) view.value = 'login'
    else await enterApp()
  } catch {
		serverOnline.value = false
    view.value = 'error'
  }
}

async function checkServer() {
	const controller = new AbortController()
	const timeout = window.setTimeout(() => controller.abort(), 3000)
	try {
		const response = await fetch('/api/health', { cache: 'no-store', signal: controller.signal })
		serverOnline.value = response.ok
	} catch { serverOnline.value = false }
	finally { window.clearTimeout(timeout) }
}

function markServerOffline() { serverOnline.value = false }

async function submitSetup() {
  formError.value = ''
  if (password.value.length < 10) return void (formError.value = '密码至少需要 10 个字符')
  if (password.value !== passwordConfirm.value) return void (formError.value = '两次输入的密码不一致')
  submitting.value = true
  try {
    await api.setup(password.value)
    password.value = ''
    passwordConfirm.value = ''
    status.value = await api.status()
    await enterApp()
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
    await enterApp()
  } catch (error) {
    formError.value = error instanceof Error ? error.message : '登录失败'
  } finally {
    submitting.value = false
  }
}

async function enterApp() {
	if (status.value?.server?.state === 'retired') { view.value = 'moved'; return }
  view.value = 'app'
  screen.value = 'chats'
	if (status.value?.device) {
		currentDeviceID.value = status.value.device.id
		localStorage.setItem('selfsend-device-id', currentDeviceID.value)
	} else {
		currentDeviceID.value = getDeviceID()
		const defaults = detectDeviceDefaults()
		const registered = await api.registerDevice({ id: currentDeviceID.value, ...defaults })
		if (legacyDefaultName(registered.name) === defaults.name && registered.name !== defaults.name) {
			await api.updateDevice(registered.id, { name: defaults.name, avatar: registered.avatar })
		}
	}
	await refreshStatus()
	await loadConversations()
  connectEvents()
}

async function logout() {
  events?.close()
  await api.logout()
  items.value = []
  devices.value = []
  view.value = 'login'
}

async function refreshStatus() { status.value = await api.status() }

async function openServerSettings() {
	screen.value = 'server'
	plusOpen.value = false
	serverLoading.value = true
	try { serverDetails.value = await api.server() }
	catch (error) { window.alert(error instanceof Error ? error.message : '无法读取服务器信息') }
	finally { serverLoading.value = false }
}

async function createMigrationReceiver() {
	if (!receiverName.value.trim() || receiverUploading.value) return
	receiverUploading.value = true
	try {
		const result = await api.createReceiver(receiverName.value.trim(), receiverAvatar.value)
		receiverToken.value = result.token
		receiverOfferURL.value = result.offer_url
		window.history.replaceState(null, '', `${window.location.pathname}${window.location.search}#receive=${encodeURIComponent(result.token)}`)
		view.value = 'receiver'
		await refreshReceiverStatus()
		startReceiverPolling()
	} catch (error) { formError.value = error instanceof Error ? error.message : '无法进入迁移接收模式' }
	finally { receiverUploading.value = false }
}

async function refreshReceiverStatus() {
	if (!receiverToken.value) return
	try {
		receiverStatus.value = await api.receiverStatus(receiverToken.value)
		if (!receiverOfferURL.value && receiverStatus.value.base_url) receiverOfferURL.value = `${receiverStatus.value.base_url}/#receive=${encodeURIComponent(receiverToken.value)}`
		if (receiverStatus.value.expected_size) receiverProgress.value = Math.min(1, (receiverStatus.value.offset || 0) / receiverStatus.value.expected_size)
		if (receiverStatus.value.state === 'active') {
			window.clearInterval(receiverTimer)
			await api.claimReceiver(receiverToken.value)
			clearHash()
			status.value = await api.status()
			await enterApp()
		}
	} catch (error) {
		if (error instanceof ApiError && error.status === 401 && receiverStatus.value?.state === 'restarting') return
	}
}

function startReceiverPolling() {
	window.clearInterval(receiverTimer)
	receiverTimer = window.setInterval(refreshReceiverStatus, 1200)
}

async function copyReceiverOffer() {
	if (!receiverOfferURL.value) return
	await copyText(receiverOfferURL.value)
	inviteCopied.value = true
	window.setTimeout(() => { inviteCopied.value = false }, 1800)
}

async function selectedBackup(event: Event) {
	const input = event.target as HTMLInputElement
	const file = input.files?.[0]
	input.value = ''
	if (!file || !receiverToken.value) return
	receiverUploading.value = true
	receiverProgress.value = 0
	try { await restoreBackup(file, receiverToken.value, (progress) => { receiverProgress.value = progress }) }
	catch (error) { window.alert(error instanceof Error ? error.message : '恢复备份失败') }
	finally { receiverUploading.value = false }
}

function openMigrationWizard() {
	migrationOffer.value = ''
	migrationPassword.value = ''
	migrationJob.value = null
	discoveredServers.value = []
	migrationScreen.value = 'destination'
	localMigrationTarget.value = 'computer'
	nasCommandCopied.value = false
	migrationOpen.value = true
	window.clearInterval(migrationTimer)
}

function selectLocalMigration() {
	migrationScreen.value = 'local-target'
}

function selectComputerMigration() {
	localMigrationTarget.value = 'computer'
	migrationScreen.value = 'transfer'
	void discoverMigrationTargets()
}

function selectNASMigration() {
	localMigrationTarget.value = 'nas'
	migrationScreen.value = 'nas-guide'
	nasCommandCopied.value = false
}

function continueNASMigration() {
	migrationScreen.value = 'transfer'
	void discoverMigrationTargets()
}

function selectOnlineMigration() {
	migrationScreen.value = 'online'
}

function backMigrationScreen() {
	if (migrationScreen.value === 'transfer' && localMigrationTarget.value === 'nas') migrationScreen.value = 'nas-guide'
	else if (migrationScreen.value === 'transfer' || migrationScreen.value === 'nas-guide') migrationScreen.value = 'local-target'
	else migrationScreen.value = 'destination'
}

async function copyNASCommand() {
	await copyText(nasInstallCommand)
	nasCommandCopied.value = true
	window.setTimeout(() => { nasCommandCopied.value = false }, 1800)
}

async function discoverMigrationTargets() {
	discoveryLoading.value = true
	try { discoveredServers.value = (await api.discoverServers()).servers.filter((server) => server.receiver) }
	catch { discoveredServers.value = [] }
	finally { discoveryLoading.value = false }
}

async function startMigration() {
	if (migrationStarting.value) return
	let parsed: URL
	try { parsed = new URL(migrationOffer.value.trim()) }
	catch { return void window.alert('请输入新服务器显示的完整迁移链接') }
	const token = new URLSearchParams(parsed.hash.replace(/^#/, '')).get('receive') || ''
	if (!token) return void window.alert('迁移链接缺少一次性凭证，请复制新服务器显示的完整链接')
	migrationStarting.value = true
	try {
		migrationJob.value = await api.startMigration(parsed.origin, token, migrationPassword.value)
		migrationPassword.value = ''
		startMigrationPolling()
	} catch (error) { window.alert(error instanceof Error ? error.message : '无法开始迁移') }
	finally { migrationStarting.value = false }
}

function startMigrationPolling() {
	window.clearInterval(migrationTimer)
	migrationTimer = window.setInterval(async () => {
		try {
			migrationJob.value = await api.migrationStatus()
			if (migrationJob.value.state === 'completed') {
				window.clearInterval(migrationTimer)
				await moveToNewServer()
			} else if (migrationJob.value.state === 'failed') window.clearInterval(migrationTimer)
		} catch { /* The old server may briefly restart or become read-only. */ }
	}, 1000)
}

async function moveToNewServer() {
	try { window.location.assign((await api.createHandoff()).url) }
	catch (error) {
		const successor = status.value?.server?.successor_url
		if (successor) window.location.assign(successor)
		else window.alert(error instanceof Error ? error.message : '请使用迁移完成页面中的新服务器地址')
	}
}

function openRollback() { rollbackPassword.value = ''; rollbackOpen.value = true }

async function rollbackMigration() {
	if (!rollbackPassword.value) return
	try {
		await api.rollbackMigration(rollbackPassword.value)
		rollbackOpen.value = false
		migrationOpen.value = false
		status.value = await api.status()
		await enterApp()
	} catch (error) { window.alert(error instanceof Error ? error.message : '无法恢复旧服务器') }
	finally { rollbackPassword.value = '' }
}

function openBackupDialog() { backupPassword.value = ''; backupOpen.value = true }

async function exportBackup() {
	if (!backupPassword.value || backupCreating.value) return
	backupCreating.value = true
	try {
		const result = await prepareBackup(backupPassword.value)
		const link = document.createElement('a')
		link.href = result.download_url
		link.download = `selfsend-backup-${new Date().toISOString().slice(0, 10)}.tar`
		link.click()
		backupOpen.value = false
	} catch (error) { window.alert(error instanceof Error ? error.message : '创建备份失败') }
	finally { backupCreating.value = false; backupPassword.value = '' }
}

function readHashToken(name: string) { return new URLSearchParams(window.location.hash.replace(/^#/, '')).get(name) || '' }
function clearHash() { window.history.replaceState(null, '', `${window.location.pathname}${window.location.search}`) }

async function loadConversations() {
  try {
		devices.value = (await api.conversations()).conversations
    if (activeDevice.value) activeDevice.value = devices.value.find((device) => device.id === activeDevice.value?.id) || activeDevice.value
  } catch (error) {
    if (error instanceof ApiError && error.status === 401) view.value = 'login'
  }
}

async function openChat(device: Conversation) {
  activeDevice.value = device
  screen.value = 'chat'
  plusOpen.value = false
  messageText.value = ''
  await loadTimeline(true)
}

function goToChats() {
  if (wideLayout.value && activeDevice.value) screen.value = 'chat'
  else {
    screen.value = 'chats'
    activeDevice.value = null
  }
  plusOpen.value = false
  void loadConversations()
}

function goToMe() {
  screen.value = 'me'
  if (!wideLayout.value) activeDevice.value = null
  plusOpen.value = false
	void Promise.all([loadConversations(), refreshStatus()])
}

async function loadTimeline(reset = false) {
  if (loadingTimeline.value || !activeDevice.value) return
  const conversationID = activeDevice.value.conversation_id
  loadingTimeline.value = true
  try {
    const response = await api.timeline(conversationID, reset ? undefined : nextCursor.value || undefined)
    if (activeDevice.value?.conversation_id !== conversationID) return
    items.value = reset ? response.items : [...items.value, ...response.items]
    nextCursor.value = response.next_cursor
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
	events.addEventListener('ready', () => { serverOnline.value = true })
	events.onerror = () => { serverOnline.value = false }
  events.addEventListener('timeline', () => {
    void loadConversations()
    if (screen.value === 'chat') void loadTimeline(true)
  })
  events.addEventListener('devices', () => loadConversations())
	events.addEventListener('friends', () => { void loadConversations() })
	events.addEventListener('conversations', () => { void loadConversations() })
	events.addEventListener('server', () => { void refreshStatus(); if (screen.value === 'server') void openServerSettings() })
	events.addEventListener('server-moved', () => { void moveToNewServer() })
}

async function openAddFriend() {
	headerMenuOpen.value = false
	addFriendOpen.value = true
	invite.value = null
	inviteCopied.value = false
	inviteLoading.value = true
	try { invite.value = await api.createPairingInvite() }
	catch (error) { window.alert(error instanceof Error ? error.message : '无法创建邀请') }
	finally { inviteLoading.value = false }
}

function openGroupCreator() {
	headerMenuOpen.value = false
	selectedGroupDeviceIDs.value = []
	groupCreatorOpen.value = true
}

function toggleGroupDevice(deviceID: string) {
	selectedGroupDeviceIDs.value = selectedGroupDeviceIDs.value.includes(deviceID)
		? selectedGroupDeviceIDs.value.filter((id) => id !== deviceID)
		: [...selectedGroupDeviceIDs.value, deviceID]
}

async function createGroup() {
	if (selectedGroupDeviceIDs.value.length < 2 || groupCreating.value) return
	groupCreating.value = true
	try {
		const conversation = await api.createGroup(selectedGroupDeviceIDs.value)
		groupCreatorOpen.value = false
		await loadConversations()
		await openChat(conversation)
	} catch (error) { window.alert(error instanceof Error ? error.message : '创建群聊失败') }
	finally { groupCreating.value = false }
}

async function copyInvite() {
	if (!inviteURL.value) return
	await copyText(inviteURL.value)
	inviteCopied.value = true
	window.setTimeout(() => { inviteCopied.value = false }, 1800)
}

async function copyText(value: string) {
	try {
		if (navigator.clipboard?.writeText) {
			await navigator.clipboard.writeText(value)
			return
		}
	} catch { /* Local HTTP may not expose the Clipboard API. */ }
	const input = document.createElement('textarea')
	input.value = value
	input.style.position = 'fixed'
	input.style.opacity = '0'
	document.body.appendChild(input)
	input.select()
	document.execCommand('copy')
	input.remove()
}

async function claimPairing() {
	if (!pairingName.value.trim() || pairingSubmitting.value) return
	pairingSubmitting.value = true
	formError.value = ''
	try {
		const result = await api.claimPairing(pairingToken.value, { name: pairingName.value.trim(), avatar: pairingAvatar.value || '设备' })
		localStorage.setItem('selfsend-device-id', result.device.id)
		window.history.replaceState(null, '', `${window.location.pathname}${window.location.search}`)
		status.value = await api.status()
		pairingDone.value = true
	} catch (error) { formError.value = error instanceof Error ? error.message : '添加失败' }
	finally { pairingSubmitting.value = false }
}

function readPairingToken() {
	const match = window.location.hash.match(/^#pair=([^&]+)/)
	if (!match) return ''
	try { return decodeURIComponent(match[1]) } catch { return '' }
}

async function finishPairing() {
	if (status.value?.authenticated) await enterApp()
	else view.value = 'login'
}

function chooseFiles() { plusOpen.value = false; fileInput.value?.click() }
function choosePhotos() { plusOpen.value = false; photoInput.value?.click() }

function selected(event: Event) {
  const target = event.target as HTMLInputElement
  if (target.files) enqueue(Array.from(target.files))
  target.value = ''
}

async function sendText() {
  const text = messageText.value.trim()
  const conversationID = activeDevice.value?.conversation_id
  if (!text || !conversationID || sendingText.value) return
  sendingText.value = true
  try {
    const item = await api.sendText(conversationID, text)
    messageText.value = ''
    plusOpen.value = false
    if (!items.value.some((candidate) => candidate.id === item.id)) items.value.unshift(item)
    await nextTick()
    resizeComposer()
    await scrollToLatest()
    composerInput.value?.focus()
    void loadConversations()
  } catch (error) {
    window.alert(error instanceof Error ? error.message : '发送失败')
  } finally {
    sendingText.value = false
  }
}

function handleComposerKeydown(event: KeyboardEvent) {
  if (event.key !== 'Enter' || event.shiftKey || event.isComposing) return
  event.preventDefault()
  void sendText()
}

function resizeComposer() {
  const input = composerInput.value
  if (!input) return
  input.style.height = 'auto'
  input.style.height = `${Math.min(input.scrollHeight, 104)}px`
}

function focusComposer() { plusOpen.value = false; composerInput.value?.focus() }

function dropped(event: DragEvent) {
  dragging.value = false
  if (screen.value === 'chat' && event.dataTransfer?.files) enqueue(Array.from(event.dataTransfer.files))
}

function enqueue(files: File[]) {
	const conversationID = activeDevice.value?.conversation_id
  if (!conversationID) return
  const max = status.value?.max_upload_size || Number.MAX_SAFE_INTEGER
  files.forEach((file) => uploads.value.unshift(file.size > max
    ? { id: taskID(), conversationID, file, progress: 0, state: 'error', error: `超过 ${formatBytes(max)} 限制` }
    : { id: taskID(), conversationID, file, progress: 0, state: 'queued' }))
  runQueue()
}

function runQueue() {
  while (activeWorkers < 2) {
    const task = uploads.value.find((item) => item.state === 'queued')
    if (!task) return
    activeWorkers++
    task.state = 'uploading'
    uploadFile(task.file, task.conversationID, (progress) => { task.progress = progress })
      .then(async () => {
        task.state = 'done'
        if (activeDevice.value?.conversation_id === task.conversationID) await loadTimeline(true)
        void loadConversations()
        window.setTimeout(() => { uploads.value = uploads.value.filter((item) => item.id !== task.id) }, 2500)
      })
      .catch((error) => { task.state = 'error'; task.error = error instanceof Error ? error.message : '上传失败' })
      .finally(() => { activeWorkers--; runQueue() })
  }
}

function retry(task: UploadTask) { task.state = 'queued'; task.error = undefined; runQueue() }

async function removeItem(item: TimelineItem) {
  const label = item.kind === 'text' ? '这条消息' : `“${item.file_name}”`
  if (!window.confirm(`永久删除${label}？`)) return
  try {
    await api.deleteItem(item.id)
    items.value = items.value.filter((candidate) => candidate.id !== item.id)
    await Promise.all([refreshStatus(), loadConversations()])
  } catch (error) { window.alert(error instanceof Error ? error.message : '删除失败') }
}

function openDeviceEditor(device = currentDevice.value) {
  if (!device) return
  editName.value = device.name
  editAvatar.value = device.avatar
  editorOpen.value = true
}

async function saveIdentity() {
  const name = editName.value.trim()
  if (!name || editSaving.value) return
  editSaving.value = true
  try {
    if (currentDevice.value) {
			const updated = await api.updateDevice(currentDevice.value.id, { name, avatar: editAvatar.value || '设备' })
			if (status.value) status.value.device = updated
			await loadConversations()
    }
    editorOpen.value = false
  } catch (error) { window.alert(error instanceof Error ? error.message : '保存失败') }
  finally { editSaving.value = false }
}

function chooseAvatarPhoto() { avatarInput.value?.click() }

async function avatarSelected(event: Event) {
  const input = event.target as HTMLInputElement
  const file = input.files?.[0]
  input.value = ''
  if (!file) return
  try { editAvatar.value = await makeAvatar(file) }
  catch { window.alert('无法读取这张图片，请换一张重试') }
}

async function makeAvatar(file: File): Promise<string> {
  if (!file.type.startsWith('image/')) throw new Error('not an image')
  const url = URL.createObjectURL(file)
  try {
    const image = await new Promise<HTMLImageElement>((resolve, reject) => {
      const element = new Image()
      element.onload = () => resolve(element)
      element.onerror = reject
      element.src = url
    })
    const size = Math.min(image.naturalWidth, image.naturalHeight)
    const canvas = document.createElement('canvas')
    canvas.width = 256
    canvas.height = 256
    const context = canvas.getContext('2d')
    if (!context) throw new Error('canvas unavailable')
    context.drawImage(image, (image.naturalWidth - size) / 2, (image.naturalHeight - size) / 2, size, size, 0, 0, 256, 256)
    return canvas.toDataURL('image/jpeg', 0.82)
  } finally { URL.revokeObjectURL(url) }
}

function isImageAvatar(avatar: string) { return avatar.startsWith('data:image/') }
function isMine(item: TimelineItem) { return item.sender_device_id === currentDeviceID.value }

function getDeviceID() {
  const stored = localStorage.getItem('selfsend-device-id')
  if (stored) return stored
  const id = globalThis.crypto?.randomUUID?.() || `device-${Date.now()}-${Math.random().toString(36).slice(2)}`
  localStorage.setItem('selfsend-device-id', id)
  return id
}

function detectDeviceDefaults(): Pick<Device, 'name' | 'avatar'> {
  const ua = navigator.userAgent
  if (/iPhone/i.test(ua)) return { name: 'iPhone', avatar: '📱' }
  if (/iPad/i.test(ua)) return { name: 'iPad', avatar: '📱' }
  if (/Android/i.test(ua)) return { name: 'Android', avatar: '📱' }
  if (/Macintosh|Mac OS/i.test(ua)) return { name: 'Mac', avatar: '💻' }
  if (/Windows/i.test(ua)) return { name: 'Windows', avatar: '🖥️' }
  return { name: '设备', avatar: '💻' }
}

function legacyDefaultName(name: string) {
  const names: Record<string, string> = {
    '我的 iPhone': 'iPhone',
    '我的 iPad': 'iPad',
    '我的安卓手机': 'Android',
    '我的 Mac': 'Mac',
    '我的 Windows 电脑': 'Windows',
    '我的设备': '设备',
  }
  return names[name] || name
}

function scrollToLatest() { return nextTick(() => { if (timeline.value) timeline.value.scrollTop = timeline.value.scrollHeight }) }
function formatBytes(bytes: number) { if (!bytes) return '0 B'; const units = ['B', 'KB', 'MB', 'GB', 'TB']; const index = Math.min(Math.floor(Math.log(bytes) / Math.log(1024)), units.length - 1); const value = bytes / Math.pow(1024, index); return `${value >= 10 || index === 0 ? value.toFixed(0) : value.toFixed(1)} ${units[index]}` }
function formatTime(timestamp: number) { return new Intl.DateTimeFormat('zh-CN', { hour: '2-digit', minute: '2-digit' }).format(new Date(timestamp)) }
function formatListTime(timestamp: number) { if (!timestamp) return ''; const date = new Date(timestamp); const today = new Date(); return date.toDateString() === today.toDateString() ? formatTime(timestamp) : `${date.getMonth() + 1}/${date.getDate()}` }
function formatDay(timestamp: number) { const date = new Date(timestamp); const today = new Date(); if (date.toDateString() === today.toDateString()) return '今天'; const yesterday = new Date(today); yesterday.setDate(today.getDate() - 1); if (date.toDateString() === yesterday.toDateString()) return '昨天'; return new Intl.DateTimeFormat('zh-CN', { month: 'long', day: 'numeric', weekday: 'short' }).format(date) }
function showDay(index: number) { if (index === 0) return true; return new Date(displayItems.value[index].created_at).toDateString() !== new Date(displayItems.value[index - 1].created_at).toDateString() }
function previewFor(conversation: Conversation) { if (!conversation.last_message_at) return conversation.kind === 'group' ? `${conversation.member_count || 0} 台设备` : '现在可以给这台设备发消息'; return conversation.last_kind === 'file' ? `[文件] ${conversation.last_preview || ''}` : conversation.last_preview || '' }
function iconFor(item: FileItem) { if (item.mime_type.startsWith('image/')) return 'IMG'; if (item.mime_type.startsWith('video/')) return 'VID'; if (item.mime_type.startsWith('audio/')) return 'AUD'; if (item.mime_type.includes('pdf')) return 'PDF'; if (item.mime_type.includes('zip') || item.mime_type.includes('compressed')) return 'ZIP'; return 'FILE' }
function canPreview(item: FileItem) { return item.mime_type.startsWith('image/') && item.size <= 15 * 1024 * 1024 }
function taskID() { return globalThis.crypto?.randomUUID?.() || `${Date.now()}-${Math.random().toString(36).slice(2)}` }
</script>

<template>
  <main v-if="view === 'loading'" class="center-screen"><div class="brand-mark loading-mark">S</div><p class="muted">正在连接 SelfSend…</p></main>

  <main v-else-if="view === 'setup' || view === 'login'" class="center-screen auth-background">
    <section class="auth-card">
      <div class="brand-mark">S</div><h1>SelfSend</h1><p class="tagline">像聊天一样，给自己的设备发消息。</p>
			<div v-if="view === 'setup' && setupMode === 'choose'" class="setup-choices"><button class="primary-button" @click="setupMode = 'create'">创建新的 SelfSend</button><button class="secondary-button setup-secondary" @click="setupMode = 'receive'">从另一台服务器迁入</button></div>
      <form v-else-if="view === 'setup' && setupMode === 'create'" @submit.prevent="submitSetup">
				<div class="welcome-note"><strong>欢迎来到你的私人传输空间</strong><span>请创建管理员密码。之后可以通过二维码添加其他设备。</span></div>
        <label>管理员密码<input v-model="password" type="password" autocomplete="new-password" minlength="10" autofocus placeholder="至少 10 个字符" /></label>
        <label>再次输入<input v-model="passwordConfirm" type="password" autocomplete="new-password" minlength="10" placeholder="确认密码" /></label>
				<p v-if="formError" class="form-error">{{ formError }}</p><button class="primary-button" :disabled="submitting">{{ submitting ? '正在创建…' : '创建私人空间' }}</button><button type="button" class="form-back-button" @click="setupMode = 'choose'">返回</button>
      </form>
			<form v-else-if="view === 'setup'" @submit.prevent="createMigrationReceiver"><div class="welcome-note"><strong>让这台设备接收旧服务器的数据</strong><span>请保持旧服务器和这台设备处于同一个局域网。</span></div><label>新服务器名称<input v-model="receiverName" maxlength="40" autocomplete="off" autofocus /></label><p v-if="formError" class="form-error">{{ formError }}</p><button class="primary-button" :disabled="receiverUploading">{{ receiverUploading ? '正在准备…' : '进入接收模式' }}</button><button type="button" class="form-back-button" @click="setupMode = 'choose'">返回</button></form>
			<form v-else @submit.prevent="submitLogin"><label>管理员密码<input v-model="password" type="password" autocomplete="current-password" autofocus placeholder="输入密码" /></label><p v-if="formError" class="form-error">{{ formError }}</p><button class="primary-button" :disabled="submitting">{{ submitting ? '正在进入…' : '进入 SelfSend' }}</button></form>
    </section>
  </main>

  <main v-else-if="view === 'pairing'" class="center-screen auth-background">
		<section class="auth-card pairing-card">
			<div class="brand-mark"><UserPlusIcon aria-hidden="true" /></div>
			<template v-if="pairingDone"><h1>设备已添加</h1><p class="tagline">现在可以和其他设备互相发送消息了。</p><button class="primary-button pairing-finish" @click="finishPairing">进入 SelfSend</button></template>
			<form v-else @submit.prevent="claimPairing">
				<h1>添加设备</h1>
				<div class="pairing-identity"><span class="person-avatar editor-avatar">{{ pairingAvatar }}</span><div class="avatar-presets compact-presets"><button v-for="preset in avatarPresets.slice(1)" :key="preset" type="button" :class="{ selected: pairingAvatar === preset }" @click="pairingAvatar = preset">{{ preset }}</button></div></div>
				<label>这台设备的名称<input v-model="pairingName" maxlength="40" autocomplete="off" autofocus /></label>
				<p v-if="formError" class="form-error">{{ formError }}</p><button class="primary-button" :disabled="!pairingName.trim() || pairingSubmitting">{{ pairingSubmitting ? '正在添加…' : '加入 SelfSend' }}</button>
			</form>
		</section>
	</main>

	<main v-else-if="view === 'receiver'" class="center-screen auth-background">
		<section class="auth-card receiver-card"><div class="brand-mark"><ServerStackIcon aria-hidden="true" /></div><h1>等待旧服务器迁入</h1><p class="tagline">在旧服务器打开“我 → 服务器 → 更换服务器”。</p><p v-if="localOnlyOrigin" class="invite-warning receiver-warning">当前链接是 localhost，旧服务器无法访问。请改用这台新服务器的局域网 IP 打开页面后重新进入接收模式。</p><div v-if="receiverOfferURL" class="qr-frame receiver-qr"><QrcodeVue :value="receiverOfferURL" :size="196" level="M" /></div><button v-if="receiverOfferURL" class="copy-invite-button" :disabled="localOnlyOrigin" @click="copyReceiverOffer"><CheckIcon v-if="inviteCopied" aria-hidden="true" /><ClipboardDocumentIcon v-else aria-hidden="true" />{{ inviteCopied ? '已复制迁移链接' : '复制迁移链接' }}</button><div class="receiver-state"><strong>{{ receiverStatus?.state === 'uploading' ? '正在接收数据' : receiverStatus?.state === 'restarting' || receiverStatus?.state === 'applying' ? '正在启动新服务器' : receiverStatus?.state === 'error' ? '迁移失败' : '等待连接' }}</strong><span v-if="receiverStatus?.state === 'uploading'">{{ Math.round(receiverProgress * 100) }}%</span><span v-else-if="receiverStatus?.error" class="error-text">{{ receiverStatus.error }}</span></div><div v-if="receiverProgress > 0" class="receiver-progress"><i :style="{ width: `${receiverProgress * 100}%` }"></i></div><label class="restore-backup-button" :class="{ disabled: receiverUploading }"><ArchiveBoxArrowDownIcon aria-hidden="true" /><span>{{ receiverUploading ? '正在恢复备份…' : '或者从备份文件恢复' }}</span><input class="visually-hidden" type="file" accept=".tar,application/x-tar" :disabled="receiverUploading" @change="selectedBackup" /></label></section>
	</main>

	<main v-else-if="view === 'moved'" class="center-screen moved-screen"><div class="brand-mark"><ArrowPathIcon aria-hidden="true" /></div><h1>SelfSend 已迁移</h1><p class="muted">这台旧服务器现在只保留一份只读副本。</p><button class="primary-button" @click="moveToNewServer">前往新服务器</button><button class="form-back-button moved-rollback" @click="openRollback">恢复这台旧服务器</button><div v-if="rollbackOpen" class="modal-backdrop"><form class="identity-modal" @submit.prevent="rollbackMigration"><div class="modal-heading"><h2>恢复旧服务器</h2><button type="button" aria-label="关闭" @click="rollbackOpen = false"><XMarkIcon aria-hidden="true" /></button></div><div class="migration-warning">只有确认新服务器没有继续接收新消息时才能恢复，否则会产生两份无法自动合并的数据。</div><label>管理员密码<input v-model="rollbackPassword" type="password" autocomplete="current-password" autofocus /></label><button class="primary-button" :disabled="!rollbackPassword">确认恢复旧服务器</button></form></div></main>

  <main v-else-if="view === 'error'" class="center-screen"><div class="brand-mark error-mark">!</div><h1>无法连接服务器</h1><p class="muted">请确认 SelfSend 服务仍在运行，然后重试。</p><button class="secondary-button" @click="bootstrap">重新连接</button></main>

	<main v-else class="app-shell" :class="{ dragging, 'wide-layout': wideLayout }" @dragenter.prevent="dragging = screen === 'chat'" @dragover.prevent="dragging = screen === 'chat'" @dragleave.self.prevent="dragging = false" @drop.prevent="dropped">
		<aside v-if="wideLayout || screen === 'chats'" class="messages-pane">
			<header class="page-topbar"><div class="page-title-block"><h1>消息</h1><span class="server-status" :class="{ offline: !serverOnline }"><i aria-hidden="true"></i>{{ serverStatusLabel }}</span></div><div class="header-action"><button class="header-add-button" aria-label="打开操作菜单" :aria-expanded="headerMenuOpen" @click="headerMenuOpen = !headerMenuOpen"><PlusCircleIcon aria-hidden="true" /></button><div v-if="headerMenuOpen" class="header-menu"><button @click="openAddFriend"><DevicePhoneMobileIcon aria-hidden="true" /><span><strong>添加设备</strong><small>扫描二维码加入</small></span></button><button @click="openGroupCreator"><UserGroupIcon aria-hidden="true" /><span><strong>拉群</strong><small>选择设备创建群聊</small></span></button></div></div></header>
      <section class="conversation-list">
				<div v-if="!devices.length" class="conversation-empty"><UserPlusIcon aria-hidden="true" /><strong>还没有其他设备</strong><button @click="openAddFriend">添加设备</button></div>
        <button v-for="device in devices" :key="device.id" class="conversation-row" :class="{ selected: screen === 'chat' && activeDevice?.conversation_id === device.conversation_id }" @click="openChat(device)">
          <span class="person-avatar conversation-avatar"><img v-if="isImageAvatar(device.avatar)" :src="device.avatar" alt="" /><span v-else>{{ device.avatar }}</span></span>
          <span class="conversation-copy"><span class="conversation-heading"><strong>{{ device.name }}</strong><time>{{ formatListTime(device.last_message_at) }}</time></span><span class="conversation-preview">{{ previewFor(device) }}</span></span>
        </button>
      </section>
      <nav class="tabbar" aria-label="主导航"><button :class="{ active: screen !== 'me' && screen !== 'server' }" @click="goToChats"><ChatBubbleOvalLeftEllipsisIcon aria-hidden="true" /><span>消息</span></button><button :class="{ active: screen === 'me' || screen === 'server' }" @click="goToMe"><UserIcon aria-hidden="true" /><span>我</span></button></nav>
    </aside>

		<section v-if="screen === 'me'" class="me-pane">
      <header class="page-topbar"><h1>我</h1></header>
      <section class="me-page">
        <button v-if="currentDevice" class="profile-card" @click="openDeviceEditor(currentDevice)"><span class="person-avatar profile-avatar"><img v-if="isImageAvatar(currentDevice.avatar)" :src="currentDevice.avatar" alt="" /><span v-else>{{ currentDevice.avatar }}</span></span><span class="profile-copy"><strong>{{ currentDevice.name }}</strong></span><ChevronRightIcon class="chevron-icon" aria-hidden="true" /></button>
        <div class="settings-group"><button class="settings-row settings-button" @click="openServerSettings"><span>服务器</span><strong>{{ status?.server?.server_device_name || '当前服务器' }}</strong><ChevronRightIcon aria-hidden="true" /></button><div class="settings-row"><span>文件存储</span><strong>{{ totalLabel }}</strong></div></div>
        <button class="logout-row" @click="logout">退出登录</button>
      </section>
			<nav v-if="!wideLayout" class="tabbar" aria-label="主导航"><button @click="goToChats"><ChatBubbleOvalLeftEllipsisIcon aria-hidden="true" /><span>消息</span></button><button class="active" @click="goToMe"><UserIcon aria-hidden="true" /><span>我</span></button></nav>
    </section>

		<section v-else-if="screen === 'server'" class="server-pane">
			<header class="chat-topbar"><button class="back-button" aria-label="返回我" @click="goToMe"><ChevronLeftIcon aria-hidden="true" /></button><div class="chat-title"><h1>服务器</h1></div><span></span></header>
			<section class="server-page"><div v-if="serverLoading" class="server-loading">正在读取服务器信息…</div><template v-else-if="serverDetails"><div class="server-hero"><span class="server-hero-icon"><ServerStackIcon aria-hidden="true" /></span><div><strong>{{ serverDetails.server.server_device_name || 'SelfSend 服务器' }}</strong><span><i aria-hidden="true"></i>在线 · {{ currentDevice?.is_server ? '这台设备是服务器' : '当前连接的服务器' }}</span></div></div><div class="server-info-group"><div><span>地址</span><strong>{{ serverDetails.server.canonical_url }}</strong></div><div><span>版本</span><strong>{{ serverDetails.version }}</strong></div><div><span>文件存储</span><strong>{{ formatBytes(serverDetails.total_bytes) }}</strong></div><div><span>服务器标识</span><strong>{{ serverDetails.server.instance_id.slice(0, 8) }}</strong></div></div><div class="server-actions"><button class="primary-button" @click="openMigrationWizard"><ArrowPathIcon aria-hidden="true" />更换服务器</button><button class="server-secondary-button" @click="openBackupDialog"><ArchiveBoxArrowDownIcon aria-hidden="true" />导出备份</button></div></template></section>
		</section>

		<section v-else-if="screen === 'chat'" class="chat-pane">
			<header class="chat-topbar"><button class="back-button" aria-label="返回消息首页" @click="goToChats"><ChevronLeftIcon aria-hidden="true" /></button><div class="chat-title"><h1>{{ activeDevice?.name }}<small v-if="activeDevice?.kind === 'group'">（{{ activeDevice.member_count }}）</small></h1></div><span></span></header>
      <section ref="timeline" class="timeline">
        <button v-if="nextCursor" class="load-more" :disabled="loadingTimeline" @click="loadTimeline(false)">{{ loadingTimeline ? '载入中…' : '查看更早的记录' }}</button>
        <div v-if="!items.length && !activeUploads.length" class="chat-empty-state"><span class="person-avatar empty-avatar"><img v-if="activeDevice && isImageAvatar(activeDevice.avatar)" :src="activeDevice.avatar" alt="" /><span v-else>{{ activeDevice?.avatar }}</span></span><h2>发点什么给 {{ activeDevice?.name }}</h2><button class="primary-button compact" @click="focusComposer">发第一条消息</button></div>
        <div class="messages">
					<template v-for="(item, index) in displayItems" :key="item.id"><div v-if="showDay(index)" class="day-separator">{{ formatDay(item.created_at) }}</div><article class="message-row" :class="{ incoming: !isMine(item) }"><span v-if="!isMine(item)" class="person-avatar message-avatar"><img v-if="isImageAvatar(item.sender_avatar)" :src="item.sender_avatar" alt="" /><span v-else>{{ item.sender_avatar || '设备' }}</span></span><div class="message-content"><div class="message-time"><span v-if="activeDevice?.kind === 'group' && !isMine(item)">{{ item.sender_name }} · </span>{{ formatTime(item.created_at) }}</div><div v-if="item.kind === 'text'" class="text-bubble">{{ item.text }}</div><div v-else class="file-bubble"><a v-if="canPreview(item)" class="image-preview" :href="`/api/files/${item.id}`" download><img :src="`/api/files/${item.id}?inline=1`" :alt="item.file_name" loading="lazy" /></a><div class="file-card"><div class="file-type" :class="iconFor(item).toLowerCase()">{{ iconFor(item) }}</div><div class="file-details"><strong :title="item.file_name">{{ item.file_name }}</strong><span>{{ formatBytes(item.size) }}</span></div><a class="download-button" :href="`/api/files/${item.id}`" download title="下载文件"><ArrowDownTrayIcon aria-hidden="true" /></a></div><button v-if="isMine(item)" class="delete-button" @click="removeItem(item)">删除</button></div><button v-if="item.kind === 'text' && isMine(item)" class="delete-button text-delete-button" @click="removeItem(item)">删除</button></div><span v-if="isMine(item) && currentDevice" class="person-avatar message-avatar"><img v-if="isImageAvatar(currentDevice.avatar)" :src="currentDevice.avatar" alt="" /><span v-else>{{ currentDevice.avatar }}</span></span></article></template>
        </div>
      </section>
      <section v-if="activeUploads.length" class="upload-panel"><article v-for="task in activeUploads" :key="task.id" class="upload-task"><div class="upload-copy"><strong>{{ task.file.name }}</strong><span v-if="task.state === 'queued'">等待上传</span><span v-else-if="task.state === 'uploading'">{{ Math.round(task.progress * 100) }}% · 请保持页面打开</span><span v-else-if="task.state === 'done'" class="success-text">上传完成</span><span v-else class="error-text">{{ task.error }}</span></div><button v-if="task.state === 'error'" class="retry-button" @click="retry(task)">重试</button><div v-if="task.state === 'uploading'" class="progress-track"><i :style="{ width: `${task.progress * 100}%` }"></i></div></article></section>
      <footer class="composer-shell"><div v-if="plusOpen" class="attachment-panel"><button class="attachment-action" @click="choosePhotos"><span class="attachment-icon photo-icon"><PhotoIcon aria-hidden="true" /></span><span>照片</span></button><button class="attachment-action" @click="chooseFiles"><span class="attachment-icon file-icon"><DocumentIcon aria-hidden="true" /></span><span>文件</span></button></div><div class="composer"><textarea ref="composerInput" v-model="messageText" rows="1" maxlength="4000" enterkeyhint="send" aria-label="输入消息" placeholder="输入消息" @input="resizeComposer" @keydown="handleComposerKeydown"></textarea><button v-if="messageText.trim()" class="send-button" :disabled="sendingText" @click="sendText">{{ sendingText ? '发送中' : '发送' }}</button><button class="plus-button" :class="{ open: plusOpen }" :aria-expanded="plusOpen" aria-label="添加照片或文件" @click="plusOpen = !plusOpen"><PlusCircleIcon aria-hidden="true" /></button><input ref="photoInput" class="visually-hidden" type="file" accept="image/*" multiple @change="selected" /><input ref="fileInput" class="visually-hidden" type="file" multiple @change="selected" /></div></footer>
		</section>

		<section v-else-if="wideLayout" class="desktop-empty-pane"><ChatBubbleOvalLeftEllipsisIcon aria-hidden="true" /><span>选择一个聊天</span></section>

		<div v-if="migrationOpen" class="modal-backdrop" @click.self="migrationJob?.state !== 'running' && (migrationOpen = false)">
			<section class="identity-modal migration-modal">
				<div class="modal-heading migration-modal-heading">
					<button v-if="!migrationJob && migrationScreen !== 'destination'" type="button" class="modal-back-button" aria-label="返回" @click="backMigrationScreen"><ChevronLeftIcon aria-hidden="true" /></button>
					<span v-else class="modal-heading-spacer"></span>
					<h2>{{ migrationTitle }}</h2>
					<button type="button" aria-label="关闭" :disabled="migrationJob?.state === 'running'" @click="migrationOpen = false"><XMarkIcon aria-hidden="true" /></button>
				</div>
				<template v-if="!migrationJob">
					<div v-if="migrationScreen === 'destination'" class="migration-choice-list">
						<button class="migration-choice" @click="selectLocalMigration"><span class="migration-choice-icon local"><ServerStackIcon aria-hidden="true" /></span><span><strong>更换为本地服务器</strong><small>迁移到同一局域网中的电脑或 NAS</small></span><ChevronRightIcon aria-hidden="true" /></button>
						<button class="migration-choice" @click="selectOnlineMigration"><span class="migration-choice-icon online"><CloudIcon aria-hidden="true" /></span><span><strong>更换为在线服务器 <em>规划中</em></strong><small>通过域名和 HTTPS 从外网访问</small></span><ChevronRightIcon aria-hidden="true" /></button>
					</div>
					<div v-else-if="migrationScreen === 'local-target'" class="migration-choice-list">
						<button class="migration-choice" @click="selectComputerMigration"><span class="migration-choice-icon computer"><ComputerDesktopIcon aria-hidden="true" /></span><span><strong>电脑</strong><small>Windows、macOS 或 Linux 电脑</small></span><ChevronRightIcon aria-hidden="true" /></button>
						<button class="migration-choice" @click="selectNASMigration"><span class="migration-choice-icon nas"><ServerStackIcon aria-hidden="true" /></span><span><strong>NAS</strong><small>群晖、威联通及其他 Docker NAS</small></span><ChevronRightIcon aria-hidden="true" /></button>
					</div>
					<div v-else-if="migrationScreen === 'nas-guide'" class="nas-guide">
						<p class="nas-guide-intro">先在 NAS 上安装 Docker 或 Container Manager，然后通过 SSH 运行以下命令。</p>
						<div class="nas-command"><pre>{{ nasInstallCommand }}</pre><button type="button" @click="copyNASCommand"><CheckIcon v-if="nasCommandCopied" aria-hidden="true" /><ClipboardDocumentIcon v-else aria-hidden="true" />{{ nasCommandCopied ? '已复制' : '复制指令' }}</button></div>
						<div class="nas-guide-steps"><p><span>1</span><strong>等待容器启动</strong><small>首次运行会从 GitHub 拉取适合 NAS 架构的镜像。</small></p><p><span>2</span><strong>打开 NAS 地址</strong><small>在浏览器访问 http://NAS局域网IP:8080。</small></p><p><span>3</span><strong>进入接收模式</strong><small>选择“从另一台服务器迁入”，复制页面生成的迁移链接。</small></p></div>
						<div class="migration-warning">NAS 需要支持 64 位 Docker。数据默认保存在名为 selfsend-data 的 Docker 卷中，删除容器不会删除数据卷。</div>
						<button class="primary-button migration-submit" @click="continueNASMigration">NAS 已启动，继续迁移</button>
					</div>
					<div v-else-if="migrationScreen === 'online'" class="online-server-placeholder">
						<span><CloudIcon aria-hidden="true" /></span><strong>在线服务器尚未开放</strong><p>当前版本只允许迁移到局域网服务器。在线模式还需要 HTTPS、域名校验和更严格的安全策略，入口先保留在这里。</p>
						<button class="secondary-button" @click="selectLocalMigration">改用本地服务器</button>
					</div>
					<div v-else class="migration-transfer-form">
						<div class="migration-steps"><span>1</span><p><strong>在新{{ localMigrationTarget === 'nas' ? ' NAS' : '电脑' }}启动一个空的 SelfSend</strong><small>首次打开时选择“从另一台服务器迁入”。</small></p></div>
						<div class="migration-steps"><span>2</span><p><strong>复制新服务器显示的迁移链接</strong><small>两台服务器需要连接同一个局域网。</small></p></div>
						<div v-if="discoveryLoading" class="discovery-note">正在寻找局域网中的新服务器…</div><div v-else-if="discoveredServers.length" class="discovery-note success">已发现 {{ discoveredServers.map((server) => server.name).join('、') }}</div>
						<label>新服务器迁移链接<input v-model="migrationOffer" autocomplete="off" placeholder="http://192.168…/#receive=…" /></label>
						<label>当前管理员密码<input v-model="migrationPassword" type="password" autocomplete="current-password" placeholder="用于确认迁移" /></label>
						<div class="migration-warning">迁移期间会暂停发送消息和上传文件。校验完成前，旧服务器不会删除任何数据。</div>
						<button class="primary-button migration-submit" :disabled="!migrationOffer.trim() || !migrationPassword || migrationStarting" @click="startMigration">{{ migrationStarting ? '正在检查…' : '开始迁移' }}</button>
					</div>
				</template>
				<template v-else><div class="migration-progress-view"><span class="migration-state-icon" :class="migrationJob.state"><ArrowPathIcon aria-hidden="true" /></span><h3>{{ migrationJob.state === 'failed' ? '迁移未完成' : migrationJob.state === 'completed' ? '迁移完成' : migrationJob.stage }}</h3><p v-if="migrationJob.target_name">目标服务器：{{ migrationJob.target_name }}</p><div v-if="migrationJob.total_bytes" class="migration-progress"><i :style="{ width: `${Math.min(100, ((migrationJob.sent_bytes || 0) / migrationJob.total_bytes) * 100)}%` }"></i></div><span v-if="migrationJob.total_bytes" class="migration-progress-copy">{{ formatBytes(migrationJob.sent_bytes || 0) }} / {{ formatBytes(migrationJob.total_bytes) }}</span><p v-if="migrationJob.error" class="form-error">{{ migrationJob.error }}</p><div v-if="migrationJob.state === 'failed'" class="migration-failed-actions"><button class="secondary-button" @click="migrationJob = null">重新尝试</button><button class="form-back-button" @click="openRollback">恢复旧服务器写入</button></div></div></template>
			</section>
		</div>

		<div v-if="rollbackOpen" class="modal-backdrop"><form class="identity-modal" @submit.prevent="rollbackMigration"><div class="modal-heading"><h2>恢复旧服务器</h2><button type="button" aria-label="关闭" @click="rollbackOpen = false"><XMarkIcon aria-hidden="true" /></button></div><div class="migration-warning">只有确认新服务器没有继续接收新消息时才能恢复，否则会产生两份无法自动合并的数据。</div><label>管理员密码<input v-model="rollbackPassword" type="password" autocomplete="current-password" autofocus /></label><button class="primary-button" :disabled="!rollbackPassword">确认恢复旧服务器</button></form></div>

		<div v-if="backupOpen" class="modal-backdrop" @click.self="!backupCreating && (backupOpen = false)"><form class="identity-modal backup-modal" @submit.prevent="exportBackup"><div class="modal-heading"><h2>导出完整备份</h2><button type="button" aria-label="关闭" :disabled="backupCreating" @click="backupOpen = false"><XMarkIcon aria-hidden="true" /></button></div><p>备份包含设备、聊天记录和所有文件，可以在一台新的 SelfSend 服务器上恢复。</p><label>管理员密码<input v-model="backupPassword" type="password" autocomplete="current-password" /></label><button class="primary-button" :disabled="!backupPassword || backupCreating">{{ backupCreating ? '正在创建备份…' : '创建并下载备份' }}</button></form></div>

    <div v-if="editorOpen" class="modal-backdrop" @click.self="editorOpen = false"><form class="identity-modal" @submit.prevent="saveIdentity"><div class="modal-heading"><h2>编辑设备账号</h2><button type="button" aria-label="关闭" @click="editorOpen = false"><XMarkIcon aria-hidden="true" /></button></div><button type="button" class="avatar-editor" @click="chooseAvatarPhoto"><span class="person-avatar editor-avatar"><img v-if="isImageAvatar(editAvatar)" :src="editAvatar" alt="" /><span v-else>{{ editAvatar }}</span></span><span>选择头像照片</span></button><input ref="avatarInput" class="visually-hidden" type="file" accept="image/*" @change="avatarSelected" /><div class="avatar-presets"><button v-for="preset in avatarPresets" :key="preset" type="button" :class="{ selected: editAvatar === preset }" @click="editAvatar = preset">{{ preset }}</button></div><label>名称<input v-model="editName" maxlength="40" autocomplete="off" /></label><button class="primary-button" :disabled="!editName.trim() || editSaving">{{ editSaving ? '保存中…' : '保存' }}</button></form></div>
		<div v-if="groupCreatorOpen" class="modal-backdrop" @click.self="groupCreatorOpen = false"><section class="identity-modal group-creator-modal"><div class="modal-heading"><h2>发起群聊</h2><button type="button" aria-label="关闭" @click="groupCreatorOpen = false"><XMarkIcon aria-hidden="true" /></button></div><div class="group-device-list"><button v-for="device in groupCandidates" :key="device.id" class="group-device-row" :class="{ selected: selectedGroupDeviceIDs.includes(device.id) }" @click="toggleGroupDevice(device.id)"><span class="selection-check">✓</span><span class="person-avatar group-device-avatar"><img v-if="isImageAvatar(device.avatar)" :src="device.avatar" alt="" /><span v-else>{{ device.avatar }}</span></span><strong>{{ device.name }}</strong></button></div><p v-if="groupCandidates.length < 2" class="group-hint">至少还需要两台设备才能创建群聊</p><button class="primary-button group-create-button" :disabled="selectedGroupDeviceIDs.length < 2 || groupCreating" @click="createGroup">{{ groupCreating ? '正在创建…' : `完成（${selectedGroupDeviceIDs.length}）` }}</button></section></div>
		<div v-if="addFriendOpen" class="modal-backdrop" @click.self="addFriendOpen = false"><section class="identity-modal add-friend-modal"><div class="modal-heading"><h2>添加设备</h2><button type="button" aria-label="关闭" @click="addFriendOpen = false"><XMarkIcon aria-hidden="true" /></button></div><div v-if="inviteLoading" class="invite-loading">正在生成邀请…</div><template v-else-if="invite"><div class="qr-frame"><QrcodeVue :value="inviteURL" :size="196" level="M" /></div><p>让新设备扫描二维码</p><p v-if="localOnlyOrigin" class="invite-warning">当前是 localhost 地址，其他设备无法访问。请先用这台电脑的局域网 IP 打开 SelfSend。</p><button class="copy-invite-button" @click="copyInvite"><CheckIcon v-if="inviteCopied" aria-hidden="true" /><ClipboardDocumentIcon v-else aria-hidden="true" />{{ inviteCopied ? '已复制' : '复制邀请链接' }}</button><small>加入后会自动出现在所有设备的消息列表中</small></template></section></div>
    <div v-if="dragging" class="drop-overlay"><div><strong>松开发给 {{ activeDevice?.name }}</strong><span>文件将保存到你的 SelfSend 服务器</span></div></div>
  </main>
</template>
