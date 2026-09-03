package com.marbarb.selfsend

import android.content.ContentResolver
import android.content.Context
import android.net.Uri
import android.provider.OpenableColumns
import android.util.Base64
import androidx.core.content.edit
import kotlinx.coroutines.Dispatchers
import kotlinx.coroutines.withContext
import org.json.JSONArray
import org.json.JSONObject
import java.io.BufferedInputStream
import java.io.BufferedOutputStream
import java.net.HttpURLConnection
import java.net.URL
import java.net.URLEncoder
import java.nio.charset.StandardCharsets

class SelfSendApi(private val context: Context) {
    private val preferences = context.getSharedPreferences("selfsend_api", Context.MODE_PRIVATE)
    var baseUrl: String = preferences.getString("current_server", "") ?: ""
        private set

    fun useServer(value: String) {
        baseUrl = normalizeServerUrl(value)
        preferences.edit { putString("current_server", baseUrl) }
    }

    fun forgetServer() {
        if (baseUrl.isNotBlank()) preferences.edit { remove(cookieKey()) }
        baseUrl = ""
        preferences.edit { remove("current_server") }
    }

    suspend fun status(): InstanceStatus = json("GET", "/api/status").toInstanceStatus()

    suspend fun setup(password: String) {
        json("POST", "/api/setup", JSONObject().put("password", password))
    }

    suspend fun login(password: String) {
        json("POST", "/api/login", JSONObject().put("password", password))
    }

    suspend fun logout() {
        request("POST", "/api/logout").close()
        preferences.edit { remove(cookieKey()) }
    }

    suspend fun registerDevice(id: String, name: String, avatar: String): Device = json(
        "POST", "/api/devices/register",
        JSONObject().put("id", id).put("name", name).put("avatar", avatar),
    ).toDevice()

    suspend fun claimPairing(token: String, name: String, avatar: String): Device =
        json(
            "POST", "/api/pairing/claim",
            JSONObject().put("token", token).put("name", name).put("avatar", avatar),
        ).getJSONObject("device").toDevice()

    suspend fun updateDevice(id: String, name: String, avatar: String): Device = json(
        "PATCH", "/api/devices/$id",
        JSONObject().put("name", name).put("avatar", avatar),
    ).toDevice()

    suspend fun conversations(): List<Conversation> =
        json("GET", "/api/conversations").getJSONArray("conversations").objects().map { it.toConversation() }

    suspend fun timeline(conversationId: String, before: Long? = null): TimelinePage {
        val encoded = URLEncoder.encode(conversationId, StandardCharsets.UTF_8.name())
        val suffix = before?.let { "&before=$it" } ?: ""
        val root = json("GET", "/api/items?conversation_id=$encoded&limit=50$suffix")
        return TimelinePage(root.getJSONArray("items").objects().map { it.toTimelineItem() }, root.optLong("next_cursor"))
    }

    suspend fun sendText(conversationId: String, text: String): TimelineItem.Text = json(
        "POST", "/api/notes", JSONObject().put("conversation_id", conversationId).put("text", text),
    ).toTimelineItem() as TimelineItem.Text

    suspend fun deleteItem(id: String) {
        request("DELETE", "/api/items/$id").close()
    }

    suspend fun createGroup(deviceIds: List<String>): Conversation = json(
        "POST", "/api/groups", JSONObject().put("device_ids", JSONArray(deviceIds)),
    ).toConversation()

    suspend fun createInvite(): String = json("POST", "/api/pairing/invites").getString("token")

    suspend fun upload(
        source: UploadSource,
        conversationId: String,
        onProgress: (Float) -> Unit,
    ) = withContext(Dispatchers.IO) {
        val fingerprint = "upload|$baseUrl|$conversationId|${source.name}|${source.size}|${source.lastModified}"
        var location = preferences.getString(fingerprint, "").orEmpty()
        var offset = 0L

        if (location.startsWith("/api/uploads/")) {
            try {
                val head = open("HEAD", location)
                val status = head.responseCode
                if (status in 200..299) offset = head.getHeaderField("Upload-Offset")?.toLongOrNull() ?: 0L
                else location = ""
                head.disconnect()
            } catch (_: Exception) {
                location = ""
            }
        }
        if (offset !in 0..source.size) {
            offset = 0
            location = ""
        }

        if (location.isBlank()) {
            val connection = open("POST", "/api/uploads").apply {
                setRequestProperty("Tus-Resumable", "1.0.0")
                setRequestProperty("Upload-Length", source.size.toString())
                setRequestProperty("Upload-Metadata", listOf(
                    "filename ${metadata64(source.name)}",
                    "filetype ${metadata64(source.mimeType)}",
                    "lastmodified ${metadata64(source.lastModified.toString())}",
                    "conversation ${metadata64(conversationId)}",
                ).joinToString(","))
            }
            val code = connection.responseCode
            if (code !in 200..299) throw apiError(connection, code)
            location = connection.getHeaderField("Location").orEmpty()
            connection.disconnect()
            if (!location.startsWith("/api/uploads/")) throw ApiException(code, "服务器返回了无效的上传地址")
            preferences.edit { putString(fingerprint, location) }
        }

        withContext(Dispatchers.Main) { onProgress(if (source.size == 0L) 1f else offset.toFloat() / source.size) }
        val chunkSize = 4 * 1024 * 1024
        while (offset < source.size) {
            val count = minOf(chunkSize.toLong(), source.size - offset).toInt()
            val bytes = ByteArray(count)
            context.contentResolver.openInputStream(source.uri).use { raw ->
                val input = BufferedInputStream(raw ?: error("无法读取所选文件"))
                skipFully(input, offset)
                var read = 0
                while (read < count) {
                    val amount = input.read(bytes, read, count - read)
                    if (amount < 0) error("文件内容比预期短")
                    read += amount
                }
            }
            val connection = open("PATCH", location).apply {
                doOutput = true
                setFixedLengthStreamingMode(count)
                setRequestProperty("Content-Type", "application/offset+octet-stream")
                setRequestProperty("Tus-Resumable", "1.0.0")
                setRequestProperty("Upload-Offset", offset.toString())
            }
            BufferedOutputStream(connection.outputStream).use { it.write(bytes) }
            val code = connection.responseCode
            if (code !in 200..299) throw apiError(connection, code)
            offset = connection.getHeaderField("Upload-Offset")?.toLongOrNull() ?: (offset + count)
            connection.disconnect()
            withContext(Dispatchers.Main) { onProgress(if (source.size == 0L) 1f else offset.toFloat() / source.size) }
        }
        preferences.edit { remove(fingerprint) }
    }

    suspend fun download(item: TimelineItem.File, destination: Uri) = withContext(Dispatchers.IO) {
        val connection = open("GET", "/api/files/${item.id}")
        val code = connection.responseCode
        if (code !in 200..299) throw apiError(connection, code)
        context.contentResolver.openOutputStream(destination, "w").use { output ->
            requireNotNull(output) { "无法写入所选位置" }
            BufferedInputStream(connection.inputStream).use { it.copyTo(output) }
        }
        connection.disconnect()
    }

    fun describe(uri: Uri): UploadSource {
        var name = "文件"
        var size = -1L
        context.contentResolver.query(uri, arrayOf(OpenableColumns.DISPLAY_NAME, OpenableColumns.SIZE), null, null, null)?.use { cursor ->
            if (cursor.moveToFirst()) {
                name = cursor.getString(0) ?: name
                if (!cursor.isNull(1)) size = cursor.getLong(1)
            }
        }
        if (size < 0) size = context.contentResolver.openAssetFileDescriptor(uri, "r")?.use { it.length } ?: -1L
        require(size >= 0) { "无法读取文件大小" }
        return UploadSource(uri, name, context.contentResolver.getType(uri) ?: "application/octet-stream", size, 0)
    }

    private suspend fun json(method: String, path: String, body: JSONObject? = null): JSONObject =
        withContext(Dispatchers.IO) {
            val response = request(method, path, body)
            try {
                val text = response.inputStream.bufferedReader().use { it.readText() }
                if (text.isBlank()) JSONObject() else JSONObject(text)
            } finally {
                response.disconnect()
            }
        }

    private suspend fun request(method: String, path: String, body: JSONObject? = null): HttpURLConnection = withContext(Dispatchers.IO) {
        val connection = open(method, path)
        if (body != null) {
            val bytes = body.toString().toByteArray(StandardCharsets.UTF_8)
            connection.doOutput = true
            connection.setFixedLengthStreamingMode(bytes.size)
            connection.setRequestProperty("Content-Type", "application/json")
            connection.outputStream.use { it.write(bytes) }
        }
        val code = connection.responseCode
        saveCookie(connection)
        if (code !in 200..299) throw apiError(connection, code)
        connection
    }

    private fun open(method: String, path: String): HttpURLConnection {
        check(baseUrl.isNotBlank()) { "请先填写服务器地址" }
        return (URL(if (path.startsWith("http")) path else "$baseUrl$path").openConnection() as HttpURLConnection).apply {
            requestMethod = method
            connectTimeout = 8_000
            readTimeout = 120_000
            useCaches = false
            preferences.getString(cookieKey(), null)?.let { setRequestProperty("Cookie", it) }
        }
    }

    private fun saveCookie(connection: HttpURLConnection) {
        val value = connection.headerFields.entries
            .firstOrNull { it.key?.equals("Set-Cookie", true) == true }?.value?.firstOrNull()
            ?.substringBefore(';') ?: return
        if (value.startsWith("selfsend_session")) preferences.edit { putString(cookieKey(), value) }
    }

    private fun cookieKey() = "cookie|$baseUrl"

    private fun apiError(connection: HttpURLConnection, status: Int): ApiException {
        val text = runCatching { connection.errorStream?.bufferedReader()?.use { it.readText() } }.getOrNull().orEmpty()
        val message = runCatching { JSONObject(text).optString("error") }.getOrNull().orEmpty()
        connection.disconnect()
        return ApiException(status, message.ifBlank { "请求失败 ($status)" })
    }

    private fun metadata64(value: String): String =
        Base64.encodeToString(value.toByteArray(StandardCharsets.UTF_8), Base64.NO_WRAP)

    private fun skipFully(input: java.io.InputStream, count: Long) {
        var remaining = count
        while (remaining > 0) {
            val skipped = input.skip(remaining)
            if (skipped > 0) remaining -= skipped
            else if (input.read() < 0) error("无法恢复文件读取位置") else remaining--
        }
    }

    companion object {
        fun normalizeServerUrl(raw: String): String {
            var value = raw.trim()
            if (!value.contains("://")) value = "http://$value"
            val url = URL(value)
            require(url.protocol == "http" || url.protocol == "https") { "只支持 HTTP 或 HTTPS 地址" }
            require(url.host.isNotBlank()) { "服务器地址无效" }
            require(url.path.isBlank() || url.path == "/") { "服务器地址不能包含路径" }
            require(url.query.isNullOrBlank()) { "服务器地址不能包含查询参数" }
            require(url.protocol == "https" || isPrivateHost(url.host)) { "公网服务器必须使用 HTTPS" }
            return URL(url.protocol, url.host, url.port, "").toString().trimEnd('/')
        }

        private fun isPrivateHost(host: String): Boolean {
            val value = host.lowercase().removePrefix("[").removeSuffix("]")
            if (value == "localhost" || '.' !in value || value.endsWith(".local") || value.endsWith(".lan") || value.endsWith(".home")) return true
            if (value == "::1" || value.startsWith("fc") || value.startsWith("fd") || value.startsWith("fe8") || value.startsWith("fe9") || value.startsWith("fea") || value.startsWith("feb")) return true
            val octets = value.split('.').mapNotNull { it.toIntOrNull() }
            if (octets.size != 4 || octets.any { it !in 0..255 }) return false
            return octets[0] == 10 || octets[0] == 127 ||
                (octets[0] == 192 && octets[1] == 168) ||
                (octets[0] == 172 && octets[1] in 16..31) ||
                (octets[0] == 169 && octets[1] == 254)
        }
    }
}

private fun HttpURLConnection.close() = disconnect()
