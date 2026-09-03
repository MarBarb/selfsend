@file:OptIn(androidx.compose.material3.ExperimentalMaterial3Api::class)

package com.marbarb.selfsend

import android.content.Intent
import androidx.activity.compose.BackHandler
import androidx.activity.compose.rememberLauncherForActivityResult
import androidx.activity.result.contract.ActivityResultContracts
import androidx.compose.foundation.BorderStroke
import androidx.compose.foundation.background
import androidx.compose.foundation.clickable
import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Box
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.ColumnScope
import androidx.compose.foundation.layout.PaddingValues
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.Spacer
import androidx.compose.foundation.layout.fillMaxSize
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.height
import androidx.compose.foundation.layout.imePadding
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.layout.size
import androidx.compose.foundation.layout.width
import androidx.compose.foundation.lazy.LazyColumn
import androidx.compose.foundation.lazy.items
import androidx.compose.foundation.lazy.rememberLazyListState
import androidx.compose.foundation.shape.CircleShape
import androidx.compose.foundation.shape.RoundedCornerShape
import androidx.compose.foundation.text.KeyboardActions
import androidx.compose.foundation.text.KeyboardOptions
import androidx.compose.material.icons.Icons
import androidx.compose.material.icons.automirrored.filled.ArrowBack
import androidx.compose.material.icons.automirrored.filled.Logout
import androidx.compose.material.icons.automirrored.filled.Send
import androidx.compose.material.icons.filled.Add
import androidx.compose.material.icons.filled.AttachFile
import androidx.compose.material.icons.filled.ChatBubbleOutline
import androidx.compose.material.icons.filled.Check
import androidx.compose.material.icons.filled.Close
import androidx.compose.material.icons.filled.Cloud
import androidx.compose.material.icons.filled.DeleteOutline
import androidx.compose.material.icons.filled.Description
import androidx.compose.material.icons.filled.Download
import androidx.compose.material.icons.filled.GroupAdd
import androidx.compose.material.icons.filled.Person
import androidx.compose.material.icons.filled.Refresh
import androidx.compose.material.icons.filled.Share
import androidx.compose.material.icons.filled.Storage
import androidx.compose.material3.AlertDialog
import androidx.compose.material3.Button
import androidx.compose.material3.Card
import androidx.compose.material3.CardDefaults
import androidx.compose.material3.CircularProgressIndicator
import androidx.compose.material3.Divider
import androidx.compose.material3.FilledIconButton
import androidx.compose.material3.HorizontalDivider
import androidx.compose.material3.Icon
import androidx.compose.material3.IconButton
import androidx.compose.material3.LinearProgressIndicator
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.NavigationBar
import androidx.compose.material3.NavigationBarItem
import androidx.compose.material3.OutlinedButton
import androidx.compose.material3.OutlinedTextField
import androidx.compose.material3.Scaffold
import androidx.compose.material3.SnackbarHost
import androidx.compose.material3.SnackbarHostState
import androidx.compose.material3.Surface
import androidx.compose.material3.Text
import androidx.compose.material3.TextButton
import androidx.compose.material3.TopAppBar
import androidx.compose.material3.TopAppBarDefaults
import androidx.compose.runtime.Composable
import androidx.compose.runtime.LaunchedEffect
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.remember
import androidx.compose.runtime.rememberCoroutineScope
import androidx.compose.runtime.saveable.rememberSaveable
import androidx.compose.runtime.setValue
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.draw.clip
import androidx.compose.ui.graphics.Color
import androidx.compose.ui.platform.LocalContext
import androidx.compose.ui.text.font.FontWeight
import androidx.compose.ui.text.input.ImeAction
import androidx.compose.ui.text.input.KeyboardType
import androidx.compose.ui.text.input.PasswordVisualTransformation
import androidx.compose.ui.text.style.TextOverflow
import androidx.compose.ui.unit.dp
import androidx.compose.ui.unit.sp
import androidx.lifecycle.viewmodel.compose.viewModel
import kotlinx.coroutines.launch
import java.text.DateFormat
import java.util.Date

private val WeChatBubble = Color(0xFF95EC69)
private val AppBackground = Color(0xFFF3F5F3)

@Composable
fun SelfSendApp(model: SelfSendViewModel = viewModel()) {
    val snackbar = remember { SnackbarHostState() }
    val message = model.errorMessage
    LaunchedEffect(message) {
        if (message != null) {
            snackbar.showSnackbar(message)
            model.clearError()
        }
    }

    Surface(Modifier.fillMaxSize(), color = AppBackground) {
        Box {
            when (model.screen) {
                AppScreen.CONNECT -> ConnectScreen(model)
                AppScreen.LOADING -> LoadingScreen()
                AppScreen.AUTH -> AuthScreen(model)
                AppScreen.CHATS -> ConversationsScreen(model)
                AppScreen.CHAT -> ChatScreen(model)
                AppScreen.PROFILE -> ProfileScreen(model)
                AppScreen.MOVED -> MovedScreen(model)
            }
            SnackbarHost(snackbar, Modifier.align(Alignment.BottomCenter).padding(16.dp))
            if (model.busy) {
                Box(
                    Modifier.fillMaxSize().background(Color.Black.copy(alpha = .08f)),
                    contentAlignment = Alignment.Center,
                ) { CircularProgressIndicator() }
            }
        }
    }
}

@Composable
private fun LoadingScreen() {
    Box(Modifier.fillMaxSize(), contentAlignment = Alignment.Center) {
        Column(horizontalAlignment = Alignment.CenterHorizontally) {
            SelfSendMark()
            Spacer(Modifier.height(24.dp))
            CircularProgressIndicator(Modifier.size(28.dp), strokeWidth = 3.dp)
            Spacer(Modifier.height(12.dp))
            Text("正在连接 SelfSend…", color = MaterialTheme.colorScheme.onSurfaceVariant)
        }
    }
}

@Composable
private fun ConnectScreen(model: SelfSendViewModel) {
    var address by rememberSaveable { mutableStateOf("") }
    AuthShell(
        title = "连接你的 SelfSend",
        subtitle = "输入服务器地址，或粘贴其他设备分享的邀请链接。",
    ) {
        OutlinedTextField(
            value = address,
            onValueChange = { address = it },
            label = { Text("服务器地址或设备邀请链接") },
            placeholder = { Text("http://192.168.1.20:8080") },
            singleLine = true,
            keyboardOptions = KeyboardOptions(keyboardType = KeyboardType.Uri, imeAction = ImeAction.Go),
            keyboardActions = KeyboardActions(onGo = { if (address.isNotBlank()) model.connect(address) }),
            modifier = Modifier.fillMaxWidth(),
        )
        Spacer(Modifier.height(12.dp))
        Button(
            onClick = { model.connect(address) },
            enabled = address.isNotBlank(),
            modifier = Modifier.fillMaxWidth().height(52.dp),
        ) { Text("连接服务器") }
        Spacer(Modifier.height(16.dp))
        Text(
            "局域网部署可以使用 HTTP；通过公网连接时请使用 HTTPS。账号、文件和消息只保存在你的服务器中。",
            style = MaterialTheme.typography.bodySmall,
            color = MaterialTheme.colorScheme.onSurfaceVariant,
        )
    }
}

@Composable
private fun AuthScreen(model: SelfSendViewModel) {
    var password by rememberSaveable { mutableStateOf("") }
    var confirmation by rememberSaveable { mutableStateOf("") }
    val setup = model.status?.setupRequired == true
    AuthShell(
        title = if (setup) "创建管理员密码" else "登录 SelfSend",
        subtitle = if (setup) "这是一个新的 SelfSend 服务器。" else (model.status?.server?.instanceName ?: model.api.baseUrl),
        onBack = model::disconnect,
    ) {
        OutlinedTextField(
            value = password,
            onValueChange = { password = it },
            label = { Text(if (setup) "管理员密码（至少 10 位）" else "管理员密码") },
            visualTransformation = PasswordVisualTransformation(),
            singleLine = true,
            keyboardOptions = KeyboardOptions(keyboardType = KeyboardType.Password, imeAction = if (setup) ImeAction.Next else ImeAction.Done),
            keyboardActions = KeyboardActions(onDone = { model.authenticate(password, confirmation) }),
            modifier = Modifier.fillMaxWidth(),
        )
        if (setup) {
            Spacer(Modifier.height(10.dp))
            OutlinedTextField(
                value = confirmation,
                onValueChange = { confirmation = it },
                label = { Text("再次输入密码") },
                visualTransformation = PasswordVisualTransformation(),
                singleLine = true,
                keyboardOptions = KeyboardOptions(keyboardType = KeyboardType.Password, imeAction = ImeAction.Done),
                keyboardActions = KeyboardActions(onDone = { model.authenticate(password, confirmation) }),
                modifier = Modifier.fillMaxWidth(),
            )
        }
        Spacer(Modifier.height(16.dp))
        Button(
            onClick = { model.authenticate(password, confirmation) },
            enabled = password.isNotBlank() && (!setup || confirmation.isNotBlank()),
            modifier = Modifier.fillMaxWidth().height(52.dp),
        ) { Text(if (setup) "创建并进入" else "登录") }
    }
}

@Composable
private fun AuthShell(title: String, subtitle: String, onBack: (() -> Unit)? = null, content: @Composable ColumnScope.() -> Unit) {
    Box(Modifier.fillMaxSize().padding(horizontal = 24.dp), contentAlignment = Alignment.Center) {
        Column(Modifier.fillMaxWidth()) {
            if (onBack != null) IconButton(onClick = onBack) { Icon(Icons.AutoMirrored.Filled.ArrowBack, "返回") }
            Column(
                Modifier.fillMaxWidth().clip(RoundedCornerShape(28.dp)).background(Color.White).padding(24.dp),
                horizontalAlignment = Alignment.CenterHorizontally,
            ) {
                SelfSendMark()
                Spacer(Modifier.height(18.dp))
                Text(title, style = MaterialTheme.typography.headlineSmall, fontWeight = FontWeight.Bold)
                Spacer(Modifier.height(6.dp))
                Text(subtitle, style = MaterialTheme.typography.bodyMedium, color = MaterialTheme.colorScheme.onSurfaceVariant)
                Spacer(Modifier.height(24.dp))
                content()
            }
        }
    }
}

@Composable
private fun SelfSendMark() {
    Box(
        Modifier.size(72.dp).clip(RoundedCornerShape(20.dp)).background(MaterialTheme.colorScheme.primary),
        contentAlignment = Alignment.Center,
    ) {
        Icon(Icons.Default.ChatBubbleOutline, null, tint = Color.White, modifier = Modifier.size(39.dp))
    }
}

@Composable
private fun ConversationsScreen(model: SelfSendViewModel) {
    var groupDialog by remember { mutableStateOf(false) }
    val context = LocalContext.current
    Scaffold(
        containerColor = AppBackground,
        topBar = {
            TopAppBar(
                title = {
                    Column {
                        Text("SelfSend", fontWeight = FontWeight.Bold)
                        Text(
                            model.status?.server?.instanceName ?: model.api.baseUrl,
                            style = MaterialTheme.typography.labelSmall,
                            color = MaterialTheme.colorScheme.onSurfaceVariant,
                            maxLines = 1,
                        )
                    }
                },
                actions = {
                    IconButton(onClick = model::refreshConversations) { Icon(Icons.Default.Refresh, "刷新") }
                    IconButton(onClick = {
                        model.createInvite { link ->
                            context.startActivity(Intent.createChooser(Intent(Intent.ACTION_SEND).apply {
                                type = "text/plain"
                                putExtra(Intent.EXTRA_SUBJECT, "加入我的 SelfSend")
                                putExtra(Intent.EXTRA_TEXT, link)
                            }, "分享设备邀请"))
                        }
                    }) { Icon(Icons.Default.Share, "邀请设备") }
                    IconButton(onClick = { groupDialog = true }) { Icon(Icons.Default.GroupAdd, "发起群聊") }
                },
                colors = TopAppBarDefaults.topAppBarColors(containerColor = Color.White),
            )
        },
        bottomBar = {
            NavigationBar(containerColor = Color.White) {
                NavigationBarItem(
                    selected = true, onClick = {}, icon = { Icon(Icons.Default.ChatBubbleOutline, null) }, label = { Text("消息") },
                )
                NavigationBarItem(
                    selected = false, onClick = model::openProfile, icon = { Icon(Icons.Default.Person, null) }, label = { Text("我") },
                )
            }
        },
    ) { padding ->
        if (model.conversations.isEmpty()) {
            Box(Modifier.fillMaxSize().padding(padding).padding(32.dp), contentAlignment = Alignment.Center) {
                Column(horizontalAlignment = Alignment.CenterHorizontally) {
                    Icon(Icons.Default.ChatBubbleOutline, null, Modifier.size(58.dp), tint = Color(0xFF9AA39D))
                    Spacer(Modifier.height(16.dp))
                    Text("还没有其他设备", fontWeight = FontWeight.SemiBold, fontSize = 18.sp)
                    Spacer(Modifier.height(6.dp))
                    Text("点击右上角分享邀请，添加你的另一台设备。", color = MaterialTheme.colorScheme.onSurfaceVariant)
                }
            }
        } else {
            LazyColumn(Modifier.fillMaxSize().padding(padding), contentPadding = PaddingValues(vertical = 8.dp)) {
                items(model.conversations, key = { it.conversationId }) { conversation ->
                    ConversationRow(conversation) { model.openConversation(conversation) }
                }
            }
        }
    }
    if (groupDialog) GroupDialog(model, onDismiss = { groupDialog = false })
}

@Composable
private fun ConversationRow(conversation: Conversation, onClick: () -> Unit) {
    Row(
        Modifier.fillMaxWidth().background(Color.White).clickable(onClick = onClick).padding(horizontal = 16.dp, vertical = 13.dp),
        verticalAlignment = Alignment.CenterVertically,
    ) {
        Avatar(conversation.avatar, 52)
        Spacer(Modifier.width(13.dp))
        Column(Modifier.weight(1f)) {
            Row(verticalAlignment = Alignment.CenterVertically) {
                Text(conversation.name, Modifier.weight(1f), fontWeight = FontWeight.SemiBold, fontSize = 17.sp, maxLines = 1)
                if (conversation.lastMessageAt > 0) Text(
                    formatTime(conversation.lastMessageAt), style = MaterialTheme.typography.labelSmall,
                    color = MaterialTheme.colorScheme.onSurfaceVariant,
                )
            }
            Spacer(Modifier.height(4.dp))
            val preview = when {
                conversation.lastMessageAt == 0L && conversation.kind == "group" -> "${conversation.memberCount ?: 0} 台设备"
                conversation.lastMessageAt == 0L -> "现在可以给这台设备发消息"
                conversation.lastKind == "file" -> "[文件] ${conversation.lastPreview.orEmpty()}"
                else -> conversation.lastPreview.orEmpty()
            }
            Text(preview, color = MaterialTheme.colorScheme.onSurfaceVariant, maxLines = 1, overflow = TextOverflow.Ellipsis)
        }
    }
    HorizontalDivider(Modifier.padding(start = 81.dp), thickness = .5.dp, color = Color(0xFFE4E8E5))
}

@Composable
private fun GroupDialog(model: SelfSendViewModel, onDismiss: () -> Unit) {
    val candidates = model.conversations.filter { it.kind == "direct" }
    var selected by remember { mutableStateOf(setOf<String>()) }
    AlertDialog(
        onDismissRequest = onDismiss,
        title = { Text("发起群聊") },
        text = {
            Column {
                if (candidates.size < 2) Text("至少还需要两台设备才能创建群聊。")
                candidates.forEach { device ->
                    Row(
                        Modifier.fillMaxWidth().clickable {
                            selected = if (device.id in selected) selected - device.id else selected + device.id
                        }.padding(vertical = 8.dp),
                        verticalAlignment = Alignment.CenterVertically,
                    ) {
                        Box(
                            Modifier.size(24.dp).clip(CircleShape).background(if (device.id in selected) MaterialTheme.colorScheme.primary else Color(0xFFE5E8E5)),
                            contentAlignment = Alignment.Center,
                        ) { if (device.id in selected) Icon(Icons.Default.Check, null, tint = Color.White, modifier = Modifier.size(17.dp)) }
                        Spacer(Modifier.width(10.dp))
                        Avatar(device.avatar, 38)
                        Spacer(Modifier.width(10.dp))
                        Text(device.name)
                    }
                }
            }
        },
        confirmButton = {
            Button(enabled = selected.size >= 2, onClick = { onDismiss(); model.createGroup(selected.toList()) }) {
                Text("完成（${selected.size}）")
            }
        },
        dismissButton = { TextButton(onClick = onDismiss) { Text("取消") } },
    )
}

@Composable
private fun ChatScreen(model: SelfSendViewModel) {
    val conversation = model.activeConversation ?: return
    var message by rememberSaveable(conversation.conversationId) { mutableStateOf("") }
    var pendingDownload by remember { mutableStateOf<TimelineItem.File?>(null) }
    val picker = rememberLauncherForActivityResult(ActivityResultContracts.OpenMultipleDocuments()) { uris ->
        if (uris.isNotEmpty()) model.uploadUris(uris)
    }
    val saveFile = rememberLauncherForActivityResult(ActivityResultContracts.CreateDocument("application/octet-stream")) { uri ->
        val item = pendingDownload
        if (uri != null && item != null) model.download(item, uri)
        pendingDownload = null
    }
    val listState = rememberLazyListState()
    val displayItems = model.timeline.asReversed()
    LaunchedEffect(displayItems.lastOrNull()?.id) {
        if (displayItems.isNotEmpty()) listState.animateScrollToItem(displayItems.lastIndex)
    }
    BackHandler { model.closeConversation() }

    Scaffold(
        containerColor = AppBackground,
        topBar = {
            TopAppBar(
                navigationIcon = { IconButton(onClick = model::closeConversation) { Icon(Icons.AutoMirrored.Filled.ArrowBack, "返回") } },
                title = {
                    Column {
                        Text(conversation.name, maxLines = 1, overflow = TextOverflow.Ellipsis)
                        if (conversation.kind == "group") Text("${conversation.memberCount ?: 0} 台设备", style = MaterialTheme.typography.labelSmall)
                    }
                },
                colors = TopAppBarDefaults.topAppBarColors(containerColor = Color.White),
            )
        },
        bottomBar = {
            Column(Modifier.background(Color.White).imePadding()) {
                model.uploads.forEach { upload ->
                    Column(Modifier.padding(horizontal = 16.dp, vertical = 6.dp)) {
                        Row { Text(upload.fileName, Modifier.weight(1f), maxLines = 1); Text("${(upload.fraction * 100).toInt()}%") }
                        LinearProgressIndicator({ upload.fraction }, Modifier.fillMaxWidth())
                    }
                }
                Row(Modifier.fillMaxWidth().padding(8.dp), verticalAlignment = Alignment.Bottom) {
                    IconButton(onClick = { picker.launch(arrayOf("*/*")) }) { Icon(Icons.Default.AttachFile, "选择文件") }
                    OutlinedTextField(
                        value = message,
                        onValueChange = { if (it.length <= 4000) message = it },
                        placeholder = { Text("输入消息") },
                        maxLines = 5,
                        keyboardOptions = KeyboardOptions(imeAction = ImeAction.Send),
                        keyboardActions = KeyboardActions(onSend = { model.sendText(message) { message = "" } }),
                        modifier = Modifier.weight(1f),
                        shape = RoundedCornerShape(22.dp),
                    )
                    Spacer(Modifier.width(6.dp))
                    FilledIconButton(
                        onClick = { model.sendText(message) { message = "" } },
                        enabled = message.isNotBlank(),
                    ) { Icon(Icons.AutoMirrored.Filled.Send, "发送") }
                }
            }
        },
    ) { padding ->
        LazyColumn(
            state = listState,
            modifier = Modifier.fillMaxSize().padding(padding),
            contentPadding = PaddingValues(horizontal = 12.dp, vertical = 12.dp),
            verticalArrangement = Arrangement.spacedBy(10.dp),
        ) {
            if (model.nextCursor > 0) item {
                TextButton(onClick = model::loadOlder, enabled = !model.loadingOlder, modifier = Modifier.fillMaxWidth()) {
                    Text(if (model.loadingOlder) "加载中…" else "加载更早消息")
                }
            }
            items(displayItems, key = { it.id }) { item ->
                MessageRow(
                    item = item,
                    mine = item.senderDeviceId == model.status?.device?.id,
                    onDownload = { file -> pendingDownload = file; saveFile.launch(file.fileName) },
                    onDelete = { model.delete(item) },
                )
            }
        }
    }
}

@Composable
private fun MessageRow(item: TimelineItem, mine: Boolean, onDownload: (TimelineItem.File) -> Unit, onDelete: () -> Unit) {
    Row(Modifier.fillMaxWidth(), horizontalArrangement = if (mine) Arrangement.End else Arrangement.Start, verticalAlignment = Alignment.Top) {
        if (!mine) {
            Avatar(item.senderAvatar, 38)
            Spacer(Modifier.width(8.dp))
        }
        Column(horizontalAlignment = if (mine) Alignment.End else Alignment.Start, modifier = Modifier.fillMaxWidth(.79f)) {
            if (!mine) Text(item.senderName, style = MaterialTheme.typography.labelSmall, color = MaterialTheme.colorScheme.onSurfaceVariant)
            Card(
                shape = RoundedCornerShape(14.dp),
                colors = CardDefaults.cardColors(containerColor = if (mine) WeChatBubble else Color.White),
                elevation = CardDefaults.cardElevation(defaultElevation = 0.dp),
            ) {
                when (item) {
                    is TimelineItem.Text -> Text(item.text, Modifier.padding(horizontal = 13.dp, vertical = 10.dp), fontSize = 16.sp)
                    is TimelineItem.File -> Row(
                        Modifier.clickable { onDownload(item) }.padding(12.dp), verticalAlignment = Alignment.CenterVertically,
                    ) {
                        Icon(Icons.Default.Description, null, Modifier.size(34.dp), tint = MaterialTheme.colorScheme.primary)
                        Spacer(Modifier.width(10.dp))
                        Column(Modifier.weight(1f)) {
                            Text(item.fileName, fontWeight = FontWeight.SemiBold, maxLines = 2, overflow = TextOverflow.Ellipsis)
                            Text(formatBytes(item.size), style = MaterialTheme.typography.labelSmall, color = Color(0xFF59625C))
                        }
                        Icon(Icons.Default.Download, "下载", Modifier.size(22.dp))
                    }
                }
            }
            Row(verticalAlignment = Alignment.CenterVertically) {
                Text(formatTime(item.createdAt), style = MaterialTheme.typography.labelSmall, color = MaterialTheme.colorScheme.onSurfaceVariant)
                if (mine) IconButton(onClick = onDelete, modifier = Modifier.size(30.dp)) {
                    Icon(Icons.Default.DeleteOutline, "删除", Modifier.size(17.dp), tint = MaterialTheme.colorScheme.onSurfaceVariant)
                }
            }
        }
        if (mine) {
            Spacer(Modifier.width(8.dp))
            Avatar(item.senderAvatar, 38)
        }
    }
}

@Composable
private fun ProfileScreen(model: SelfSendViewModel) {
    val current = model.status?.device
    var name by rememberSaveable(current?.id) { mutableStateOf(current?.name.orEmpty()) }
    var avatar by rememberSaveable(current?.id) { mutableStateOf(current?.avatar ?: "📱") }
    val avatars = listOf("我", "📱", "💻", "🖥️", "📂", "🟢", "🐼", "🐱")
    BackHandler { model.closeProfile() }
    Scaffold(
        containerColor = AppBackground,
        topBar = {
            TopAppBar(
                navigationIcon = { IconButton(onClick = model::closeProfile) { Icon(Icons.AutoMirrored.Filled.ArrowBack, "返回") } },
                title = { Text("我", fontWeight = FontWeight.Bold) },
                colors = TopAppBarDefaults.topAppBarColors(containerColor = Color.White),
            )
        },
    ) { padding ->
        LazyColumn(Modifier.fillMaxSize().padding(padding), contentPadding = PaddingValues(16.dp), verticalArrangement = Arrangement.spacedBy(14.dp)) {
            item {
                Card(colors = CardDefaults.cardColors(containerColor = Color.White), shape = RoundedCornerShape(18.dp)) {
                    Column(Modifier.padding(18.dp)) {
                        Text("设备账号", style = MaterialTheme.typography.titleMedium, fontWeight = FontWeight.Bold)
                        Spacer(Modifier.height(16.dp))
                        Row(Modifier.fillMaxWidth(), horizontalArrangement = Arrangement.SpaceBetween) {
                            avatars.forEach { value ->
                                Box(
                                    Modifier.size(38.dp).clip(CircleShape)
                                        .background(if (avatar == value) MaterialTheme.colorScheme.primaryContainer else Color(0xFFF0F2F0))
                                        .clickable { avatar = value },
                                    contentAlignment = Alignment.Center,
                                ) { Text(value) }
                            }
                        }
                        Spacer(Modifier.height(14.dp))
                        OutlinedTextField(name, { name = it.take(40) }, label = { Text("设备名称") }, singleLine = true, modifier = Modifier.fillMaxWidth())
                        Spacer(Modifier.height(12.dp))
                        Button(onClick = { model.saveProfile(name, avatar) }, modifier = Modifier.fillMaxWidth()) { Text("保存") }
                    }
                }
            }
            item {
                Card(colors = CardDefaults.cardColors(containerColor = Color.White), shape = RoundedCornerShape(18.dp)) {
                    Column(Modifier.padding(18.dp)) {
                        Row(verticalAlignment = Alignment.CenterVertically) {
                            Icon(if (model.status?.server?.deploymentType == "cloud") Icons.Default.Cloud else Icons.Default.Storage, null)
                            Spacer(Modifier.width(10.dp))
                            Column(Modifier.weight(1f)) {
                                Text(model.status?.server?.instanceName ?: "当前服务器", fontWeight = FontWeight.Bold)
                                Text(model.api.baseUrl, style = MaterialTheme.typography.bodySmall, color = MaterialTheme.colorScheme.onSurfaceVariant)
                            }
                        }
                        Spacer(Modifier.height(12.dp))
                        Text("${model.status?.itemCount ?: 0} 条内容 · ${formatBytes(model.status?.totalBytes ?: 0)}", color = MaterialTheme.colorScheme.onSurfaceVariant)
                        Spacer(Modifier.height(16.dp))
                        OutlinedButton(onClick = model::logout, modifier = Modifier.fillMaxWidth()) {
                            Icon(Icons.AutoMirrored.Filled.Logout, null); Spacer(Modifier.width(8.dp)); Text("退出登录")
                        }
                        TextButton(onClick = model::disconnect, modifier = Modifier.fillMaxWidth()) { Text("切换服务器") }
                    }
                }
            }
        }
    }
}

@Composable
private fun MovedScreen(model: SelfSendViewModel) {
    AuthShell("服务器已迁移", "这台旧服务器已经停止接收新消息。", onBack = model::disconnect) {
        val successor = model.status?.server?.successorUrl
        Text(successor ?: "请在新的 SelfSend 服务器继续使用。", color = MaterialTheme.colorScheme.onSurfaceVariant)
        if (!successor.isNullOrBlank()) {
            Spacer(Modifier.height(14.dp))
            Button(onClick = { model.connect(successor) }, modifier = Modifier.fillMaxWidth()) { Text("前往新服务器") }
        }
    }
}

@Composable
private fun Avatar(value: String, size: Int) {
    Box(
        Modifier.size(size.dp).clip(RoundedCornerShape((size / 3).dp)).background(Color(0xFFE4F7E9)),
        contentAlignment = Alignment.Center,
    ) { Text(if (value.startsWith("data:image/")) "设备" else value.ifBlank { "设备" }, fontSize = (size * .42).sp, maxLines = 1) }
}

private fun formatTime(milliseconds: Long): String =
    if (milliseconds <= 0) "" else DateFormat.getDateTimeInstance(DateFormat.SHORT, DateFormat.SHORT).format(Date(milliseconds))

private fun formatBytes(bytes: Long): String {
    if (bytes < 1024) return "$bytes B"
    val units = arrayOf("KB", "MB", "GB", "TB")
    var value = bytes.toDouble()
    var unit = -1
    do { value /= 1024; unit++ } while (value >= 1024 && unit < units.lastIndex)
    return if (value >= 10) "%.0f %s".format(value, units[unit]) else "%.1f %s".format(value, units[unit])
}
