package com.marbarb.selfsend

data class ServerIdentity(
    val instanceId: String,
    val instanceName: String,
    val canonicalUrl: String,
    val state: String,
    val successorUrl: String?,
    val deploymentType: String,
    val provider: String?,
)

data class Device(
    val id: String,
    val name: String,
    val avatar: String,
    val createdAt: Long,
    val lastSeenAt: Long,
    val isServer: Boolean,
)

data class InstanceStatus(
    val setupRequired: Boolean,
    val authenticated: Boolean,
    val maxUploadSize: Long,
    val version: String,
    val itemCount: Long,
    val totalBytes: Long,
    val device: Device?,
    val server: ServerIdentity?,
)

data class Conversation(
    val id: String,
    val conversationId: String,
    val kind: String,
    val name: String,
    val avatar: String,
    val createdAt: Long,
    val lastMessageAt: Long,
    val lastKind: String?,
    val lastPreview: String?,
    val memberCount: Int?,
    val isServer: Boolean,
)

sealed interface TimelineItem {
    val id: String
    val createdAt: Long
    val senderDeviceId: String
    val senderName: String
    val senderAvatar: String

    data class Text(
        override val id: String,
        val text: String,
        override val createdAt: Long,
        override val senderDeviceId: String,
        override val senderName: String,
        override val senderAvatar: String,
    ) : TimelineItem

    data class File(
        override val id: String,
        val fileName: String,
        val mimeType: String,
        val size: Long,
        val sha256: String,
        val lastModified: Long?,
        override val createdAt: Long,
        override val senderDeviceId: String,
        override val senderName: String,
        override val senderAvatar: String,
    ) : TimelineItem
}

data class TimelinePage(val items: List<TimelineItem>, val nextCursor: Long)

data class UploadSource(
    val uri: android.net.Uri,
    val name: String,
    val mimeType: String,
    val size: Long,
    val lastModified: Long,
)

data class UploadProgress(val fileName: String, val fraction: Float)

class ApiException(val status: Int, message: String) : Exception(message)
