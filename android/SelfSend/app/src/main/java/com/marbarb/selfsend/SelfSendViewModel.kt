package com.marbarb.selfsend

import android.app.Application
import android.net.Uri
import android.os.Build
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableStateListOf
import androidx.compose.runtime.mutableLongStateOf
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.setValue
import androidx.lifecycle.AndroidViewModel
import androidx.lifecycle.viewModelScope
import androidx.core.content.edit
import androidx.core.net.toUri
import kotlinx.coroutines.Job
import kotlinx.coroutines.delay
import kotlinx.coroutines.isActive
import kotlinx.coroutines.launch
import java.util.UUID

enum class AppScreen { CONNECT, LOADING, AUTH, CHATS, CHAT, PROFILE, MOVED }

class SelfSendViewModel(application: Application) : AndroidViewModel(application) {
    val api = SelfSendApi(application)
    private val preferences = application.getSharedPreferences("selfsend_state", 0)

    var screen by mutableStateOf(AppScreen.LOADING)
        private set
    var status by mutableStateOf<InstanceStatus?>(null)
        private set
    var conversations by mutableStateOf<List<Conversation>>(emptyList())
        private set
    var activeConversation by mutableStateOf<Conversation?>(null)
        private set
    var timeline by mutableStateOf<List<TimelineItem>>(emptyList())
        private set
    var nextCursor by mutableLongStateOf(0L)
        private set
    var busy by mutableStateOf(false)
        private set
    var loadingOlder by mutableStateOf(false)
        private set
    var errorMessage by mutableStateOf<String?>(null)
        private set
    val uploads = mutableStateListOf<UploadProgress>()

    private var pollJob: Job? = null

    init { bootstrap() }

    fun bootstrap() {
        if (api.baseUrl.isBlank()) {
            screen = AppScreen.CONNECT
            return
        }
        screen = AppScreen.LOADING
        launchTask {
            val loaded = api.status()
            status = loaded
            when {
                loaded.server?.state == "retired" -> screen = AppScreen.MOVED
                loaded.setupRequired || !loaded.authenticated -> screen = AppScreen.AUTH
                else -> enterApp(loaded)
            }
        }
    }

    fun connect(address: String) {
        val possibleUrl = if (address.contains("://")) address.trim() else "http://${address.trim()}"
        val parsed = runCatching { possibleUrl.toUri() }.getOrNull()
        val pairingToken = parsed?.fragment?.split('&')?.firstOrNull { it.startsWith("pair=") }?.substringAfter("pair=")
        if (!pairingToken.isNullOrBlank() && parsed.scheme != null && parsed.encodedAuthority != null) {
            runCatching { api.useServer("${parsed.scheme}://${parsed.encodedAuthority}") }
                .onFailure { errorMessage = messageFor(it) }
                .onSuccess {
                    screen = AppScreen.LOADING
                    launchTask(showBusy = true) {
                        api.claimPairing(pairingToken, Build.MODEL.take(40).ifBlank { "Android 设备" }, "📱")
                        val loaded = api.status()
                        status = loaded
                        enterApp(loaded)
                    }
                }
            return
        }
        runCatching { api.useServer(address) }
            .onFailure { errorMessage = messageFor(it) }
            .onSuccess { bootstrap() }
    }

    fun disconnect() {
        pollJob?.cancel()
        api.forgetServer()
        status = null
        conversations = emptyList()
        timeline = emptyList()
        activeConversation = null
        screen = AppScreen.CONNECT
    }

    fun authenticate(password: String, confirmation: String = "") {
        val current = status ?: return
        if (password.length < 10) {
            errorMessage = "密码至少需要 10 个字符"
            return
        }
        if (current.setupRequired && password != confirmation) {
            errorMessage = "两次输入的密码不一致"
            return
        }
        launchTask(showBusy = true) {
            if (current.setupRequired) api.setup(password) else api.login(password)
            val loaded = api.status()
            status = loaded
            enterApp(loaded)
        }
    }

    private suspend fun enterApp(initial: InstanceStatus) {
        var loaded = initial
        if (loaded.device == null) {
            val key = "device|${loaded.server?.instanceId ?: api.baseUrl}"
            val id = preferences.getString(key, null) ?: UUID.randomUUID().toString().also {
                preferences.edit { putString(key, it) }
            }
            api.registerDevice(id, Build.MODEL.take(40).ifBlank { "Android 设备" }, "📱")
            loaded = api.status()
            status = loaded
        }
        conversations = api.conversations()
        screen = AppScreen.CHATS
        startConversationPolling()
    }

    fun refreshConversations() = launchTask { conversations = api.conversations() }

    fun openConversation(conversation: Conversation) {
        activeConversation = conversation
        timeline = emptyList()
        nextCursor = 0
        screen = AppScreen.CHAT
        launchTask(showBusy = true) { loadTimelinePage(false) }
        startTimelinePolling()
    }

    fun closeConversation() {
        activeConversation = null
        timeline = emptyList()
        screen = AppScreen.CHATS
        startConversationPolling()
        refreshConversations()
    }

    fun loadOlder() {
        if (nextCursor <= 0 || loadingOlder) return
        viewModelScope.launch {
            loadingOlder = true
            try { loadTimelinePage(true) } catch (error: Throwable) { errorMessage = messageFor(error) }
            finally { loadingOlder = false }
        }
    }

    private suspend fun loadTimelinePage(older: Boolean) {
        val conversation = activeConversation ?: return
        val page = api.timeline(conversation.conversationId, if (older) nextCursor else null)
        timeline = if (older) timeline + page.items.filterNot { old -> timeline.any { it.id == old.id } } else page.items
        nextCursor = page.nextCursor
    }

    fun sendText(text: String, afterSend: () -> Unit) {
        val conversation = activeConversation ?: return
        val value = text.trim()
        if (value.isBlank()) return
        launchTask {
            api.sendText(conversation.conversationId, value)
            loadTimelinePage(false)
            afterSend()
        }
    }

    fun uploadUris(uris: List<Uri>) {
        val conversation = activeConversation ?: return
        viewModelScope.launch {
            for (uri in uris) {
                try {
                    val source = api.describe(uri)
                    if (source.size > (status?.maxUploadSize ?: Long.MAX_VALUE)) throw IllegalArgumentException("${source.name} 超过服务器文件大小限制")
                    uploads.add(UploadProgress(source.name, 0f))
                    api.upload(source, conversation.conversationId) { fraction ->
                        val index = uploads.indexOfFirst { it.fileName == source.name }
                        if (index >= 0) uploads[index] = UploadProgress(source.name, fraction)
                    }
                    uploads.removeAll { it.fileName == source.name }
                    loadTimelinePage(false)
                } catch (error: Throwable) {
                    uploads.clear()
                    errorMessage = messageFor(error)
                }
            }
        }
    }

    fun download(item: TimelineItem.File, destination: Uri) = launchTask(showBusy = true) {
        api.download(item, destination)
    }

    fun delete(item: TimelineItem) = launchTask {
        api.deleteItem(item.id)
        loadTimelinePage(false)
    }

    fun createGroup(deviceIds: List<String>) = launchTask(showBusy = true) {
        val group = api.createGroup(deviceIds)
        conversations = api.conversations()
        openConversation(group)
    }

    fun createInvite(onReady: (String) -> Unit) = launchTask(showBusy = true) {
        val token = api.createInvite()
        onReady("${api.baseUrl}/#pair=$token")
    }

    fun openProfile() {
        pollJob?.cancel()
        screen = AppScreen.PROFILE
    }

    fun closeProfile() {
        screen = AppScreen.CHATS
        startConversationPolling()
    }

    fun saveProfile(name: String, avatar: String) {
        val device = status?.device ?: return
        if (name.trim().isBlank()) {
            errorMessage = "设备名称不能为空"
            return
        }
        launchTask(showBusy = true) {
            api.updateDevice(device.id, name.trim(), avatar)
            status = api.status()
            screen = AppScreen.CHATS
            conversations = api.conversations()
            startConversationPolling()
        }
    }

    fun logout() = launchTask(showBusy = true) {
        pollJob?.cancel()
        api.logout()
        status = api.status()
        screen = AppScreen.AUTH
    }

    fun clearError() { errorMessage = null }

    private fun startConversationPolling() {
        pollJob?.cancel()
        pollJob = viewModelScope.launch {
            while (isActive && screen == AppScreen.CHATS) {
                delay(5_000)
                runCatching { api.conversations() }.onSuccess { conversations = it }
            }
        }
    }

    private fun startTimelinePolling() {
        pollJob?.cancel()
        pollJob = viewModelScope.launch {
            while (isActive && screen == AppScreen.CHAT) {
                delay(3_000)
                runCatching { loadTimelinePage(false) }
            }
        }
    }

    private fun launchTask(showBusy: Boolean = false, block: suspend () -> Unit) {
        viewModelScope.launch {
            if (showBusy) busy = true
            errorMessage = null
            try { block() }
            catch (error: Throwable) {
                if (error is ApiException && error.status == 401 && api.baseUrl.isNotBlank()) {
                    status = runCatching { api.status() }.getOrNull()
                    screen = AppScreen.AUTH
                }
                errorMessage = messageFor(error)
            } finally { if (showBusy) busy = false }
        }
    }

    private fun messageFor(error: Throwable): String = when (error) {
        is ApiException -> error.message ?: "服务器请求失败"
        is java.net.ConnectException -> "无法连接 SelfSend 服务器"
        is java.net.SocketTimeoutException -> "连接服务器超时"
        else -> error.message ?: "操作失败"
    }
}
