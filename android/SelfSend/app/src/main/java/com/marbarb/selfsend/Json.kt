package com.marbarb.selfsend

import org.json.JSONArray
import org.json.JSONObject

internal fun JSONObject.string(name: String): String = optString(name, "")
internal fun JSONObject.nullableString(name: String): String? =
    if (isNull(name)) null else optString(name).takeIf { it.isNotBlank() }

internal fun JSONObject.toDevice() = Device(
    id = string("id"),
    name = string("name"),
    avatar = string("avatar"),
    createdAt = optLong("created_at"),
    lastSeenAt = optLong("last_seen_at"),
    isServer = optBoolean("is_server"),
)

internal fun JSONObject.toServerIdentity() = ServerIdentity(
    instanceId = string("instance_id"),
    instanceName = string("instance_name"),
    canonicalUrl = string("canonical_url"),
    state = string("state"),
    successorUrl = nullableString("successor_url"),
    deploymentType = string("deployment_type").ifBlank { "local" },
    provider = nullableString("provider"),
)

internal fun JSONObject.toInstanceStatus() = InstanceStatus(
    setupRequired = optBoolean("setup_required"),
    authenticated = optBoolean("authenticated"),
    maxUploadSize = optLong("max_upload_size"),
    version = string("version"),
    itemCount = optLong("item_count"),
    totalBytes = optLong("total_bytes"),
    device = optJSONObject("device")?.toDevice(),
    server = optJSONObject("server")?.toServerIdentity(),
)

internal fun JSONObject.toConversation() = Conversation(
    id = string("id"),
    conversationId = string("conversation_id"),
    kind = string("kind"),
    name = string("name"),
    avatar = string("avatar"),
    createdAt = optLong("created_at"),
    lastMessageAt = optLong("last_message_at"),
    lastKind = nullableString("last_kind"),
    lastPreview = nullableString("last_preview"),
    memberCount = if (isNull("member_count")) null else optInt("member_count"),
    isServer = optBoolean("is_server"),
)

internal fun JSONObject.toTimelineItem(): TimelineItem = when (string("kind")) {
    "text" -> TimelineItem.Text(
        id = string("id"), text = string("text"), createdAt = optLong("created_at"),
        senderDeviceId = string("sender_device_id"), senderName = string("sender_name"),
        senderAvatar = string("sender_avatar"),
    )
    "file" -> TimelineItem.File(
        id = string("id"), fileName = string("file_name"), mimeType = string("mime_type"),
        size = optLong("size"), sha256 = string("sha256"),
        lastModified = if (isNull("last_modified")) null else optLong("last_modified"),
        createdAt = optLong("created_at"), senderDeviceId = string("sender_device_id"),
        senderName = string("sender_name"), senderAvatar = string("sender_avatar"),
    )
    else -> error("未知消息类型")
}

internal fun JSONArray.objects(): List<JSONObject> =
    (0 until length()).map { getJSONObject(it) }
