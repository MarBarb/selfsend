use std::{
    collections::HashMap,
    net::IpAddr,
    path::{Path, PathBuf},
    str::FromStr,
    sync::{
        atomic::{AtomicU64, Ordering},
        RwLock,
    },
    time::{Duration, SystemTime, UNIX_EPOCH},
};

use base64::{engine::general_purpose::STANDARD, Engine as _};
use futures_util::StreamExt;
use reqwest::{
    header::{HeaderMap, COOKIE, SET_COOKIE},
    Client, Method, Response, StatusCode,
};
use serde::{Deserialize, Serialize};
use serde_json::Value;
use sha2::{Digest, Sha256};
use tauri::{
    menu::{Menu, MenuItem},
    tray::{MouseButton, MouseButtonState, TrayIconBuilder, TrayIconEvent},
    AppHandle, Emitter, Manager, State, WebviewUrl, WebviewWindowBuilder,
};
use tauri_plugin_autostart::{MacosLauncher, ManagerExt as AutostartManagerExt};
use tauri_plugin_notification::NotificationExt;
use tokio::io::{AsyncReadExt, AsyncSeekExt, AsyncWriteExt};
use url::{Host, Url};

const CREDENTIAL_SERVICE: &str = "SelfSend Windows";
const CHUNK_SIZE: usize = 4 * 1024 * 1024;

#[derive(Clone, Default, Deserialize, Serialize)]
struct DesktopSettings {
    base_url: Option<String>,
    #[serde(default)]
    autostart_initialized: bool,
    #[serde(default)]
    uploads: HashMap<String, String>,
}

#[derive(Clone, Default)]
struct RuntimeState {
    settings: DesktopSettings,
    cookie: Option<String>,
}

struct AppState {
    request_client: Client,
    stream_client: Client,
    runtime: RwLock<RuntimeState>,
    monitor_generation: AtomicU64,
}

impl AppState {
    fn new() -> Self {
        let request_client = Client::builder()
            .connect_timeout(Duration::from_secs(8))
            .timeout(Duration::from_secs(120))
            .user_agent("SelfSend-Windows/0.1.0")
            .build()
            .expect("build HTTP client");
        let stream_client = Client::builder()
            .connect_timeout(Duration::from_secs(8))
            .user_agent("SelfSend-Windows/0.1.0")
            .build()
            .expect("build event client");
        Self {
            request_client,
            stream_client,
            runtime: RwLock::new(RuntimeState::default()),
            monitor_generation: AtomicU64::new(0),
        }
    }
}

#[derive(Deserialize)]
struct ApiRequest {
    method: String,
    path: String,
    body: Option<Value>,
}

#[derive(Serialize)]
struct DesktopBootstrap {
    configured: bool,
    base_url: Option<String>,
    autostart_enabled: bool,
}

#[derive(Clone, Serialize)]
struct ServerEvent {
    kind: String,
}

#[derive(Clone, Serialize)]
struct UploadProgress {
    file_name: String,
    fraction: f64,
}

#[tauri::command]
fn desktop_bootstrap(
    app: AppHandle,
    state: State<'_, AppState>,
) -> Result<DesktopBootstrap, String> {
    let runtime = state.runtime.read().map_err(|_| "客户端状态不可用")?;
    let enabled = app.autolaunch().is_enabled().unwrap_or(false);
    Ok(DesktopBootstrap {
        configured: runtime.settings.base_url.is_some(),
        base_url: runtime.settings.base_url.clone(),
        autostart_enabled: enabled,
    })
}

#[tauri::command]
fn configure_server(
    app: AppHandle,
    state: State<'_, AppState>,
    address: String,
) -> Result<String, String> {
    let base_url = normalize_server_url(&address)?;
    let cookie = load_cookie(&base_url);
    {
        let mut runtime = state.runtime.write().map_err(|_| "客户端状态不可用")?;
        runtime.settings.base_url = Some(base_url.clone());
        runtime.settings.uploads.clear();
        runtime.cookie = cookie;
    }
    save_current_settings(&app, &state)?;
    start_event_monitor(app);
    Ok(base_url)
}

#[tauri::command]
fn clear_server(app: AppHandle, state: State<'_, AppState>) -> Result<(), String> {
    let old_base = {
        let mut runtime = state.runtime.write().map_err(|_| "客户端状态不可用")?;
        let old = runtime.settings.base_url.take();
        runtime.settings.uploads.clear();
        runtime.cookie = None;
        old
    };
    state.monitor_generation.fetch_add(1, Ordering::SeqCst);
    if let Some(base_url) = old_base {
        delete_cookie(&base_url);
    }
    save_current_settings(&app, &state)
}

#[tauri::command]
fn autostart_enabled(app: AppHandle) -> bool {
    app.autolaunch().is_enabled().unwrap_or(false)
}

#[tauri::command]
fn set_autostart(app: AppHandle, state: State<'_, AppState>, enabled: bool) -> Result<(), String> {
    if enabled {
        app.autolaunch()
            .enable()
            .map_err(|error| format!("无法启用自动启动：{error}"))?;
    } else {
        app.autolaunch()
            .disable()
            .map_err(|error| format!("无法关闭自动启动：{error}"))?;
    }
    {
        let mut runtime = state.runtime.write().map_err(|_| "客户端状态不可用")?;
        runtime.settings.autostart_initialized = true;
    }
    save_current_settings(&app, &state)
}

#[tauri::command]
async fn api_request(
    app: AppHandle,
    state: State<'_, AppState>,
    request: ApiRequest,
) -> Result<Value, String> {
    let method = Method::from_str(request.method.trim()).map_err(|_| "不支持的请求方法")?;
    if !matches!(
        method,
        Method::GET | Method::POST | Method::PATCH | Method::DELETE | Method::HEAD
    ) {
        return Err("不支持的请求方法".into());
    }
    let (url, cookie) = endpoint_and_cookie(&state, &request.path)?;
    let mut builder = state.request_client.request(method, url);
    if let Some(cookie) = cookie {
        builder = builder.header(COOKIE, cookie);
    }
    if let Some(body) = request.body {
        builder = builder.json(&body);
    }
    let response = builder.send().await.map_err(network_error)?;
    let cookie_changed = capture_session_cookie(&app, &state, response.headers())?;
    let result = json_response(response).await;
    if cookie_changed {
        start_event_monitor(app);
    }
    result
}

#[tauri::command]
async fn upload_files(
    app: AppHandle,
    state: State<'_, AppState>,
    paths: Vec<String>,
    conversation_id: String,
) -> Result<(), String> {
    if conversation_id.is_empty() || conversation_id.len() > 160 {
        return Err("会话标识无效".into());
    }
    for path in paths {
        upload_one(&app, &state, Path::new(&path), &conversation_id).await?;
    }
    Ok(())
}

#[tauri::command]
async fn download_file(
    state: State<'_, AppState>,
    item_id: String,
    destination: String,
) -> Result<(), String> {
    if !item_id
        .chars()
        .all(|value| value.is_ascii_alphanumeric() || value == '-' || value == '_')
    {
        return Err("文件标识无效".into());
    }
    let (url, cookie) = endpoint_and_cookie(&state, &format!("/api/files/{item_id}"))?;
    let mut builder = state.request_client.get(url);
    if let Some(cookie) = cookie {
        builder = builder.header(COOKIE, cookie);
    }
    let response = builder.send().await.map_err(network_error)?;
    if !response.status().is_success() {
        return Err(response_error(response).await);
    }

    let destination = PathBuf::from(destination);
    let file_name = destination
        .file_name()
        .and_then(|value| value.to_str())
        .unwrap_or("download");
    let temporary = destination.with_file_name(format!(".{file_name}.selfsend-part"));
    let mut output = tokio::fs::File::create(&temporary)
        .await
        .map_err(|error| format!("无法创建下载文件：{error}"))?;
    let mut stream = response.bytes_stream();
    while let Some(chunk) = stream.next().await {
        let chunk = chunk.map_err(network_error)?;
        if let Err(error) = output.write_all(&chunk).await {
            let _ = tokio::fs::remove_file(&temporary).await;
            return Err(format!("无法写入下载文件：{error}"));
        }
    }
    output
        .flush()
        .await
        .map_err(|error| format!("无法保存下载文件：{error}"))?;
    drop(output);
    if tokio::fs::try_exists(&destination).await.unwrap_or(false) {
        tokio::fs::remove_file(&destination)
            .await
            .map_err(|error| format!("无法覆盖原文件：{error}"))?;
    }
    tokio::fs::rename(&temporary, &destination)
        .await
        .map_err(|error| format!("无法完成下载：{error}"))
}

async fn upload_one(
    app: &AppHandle,
    state: &AppState,
    path: &Path,
    conversation_id: &str,
) -> Result<(), String> {
    let metadata = tokio::fs::metadata(path)
        .await
        .map_err(|error| format!("无法读取文件：{error}"))?;
    if !metadata.is_file() {
        return Err("只能发送普通文件".into());
    }
    let size = metadata.len();
    let file_name = path
        .file_name()
        .and_then(|value| value.to_str())
        .unwrap_or("文件")
        .to_owned();
    let modified = metadata
        .modified()
        .unwrap_or(SystemTime::UNIX_EPOCH)
        .duration_since(UNIX_EPOCH)
        .unwrap_or_default()
        .as_millis();
    let fingerprint = upload_fingerprint(state, path, conversation_id, size, modified)?;

    let saved_location = state
        .runtime
        .read()
        .map_err(|_| "客户端状态不可用")?
        .settings
        .uploads
        .get(&fingerprint)
        .cloned();
    let mut location = saved_location.unwrap_or_default();
    let mut offset = 0_u64;

    if location.starts_with("/api/uploads/") {
        let (head_url, cookie) = endpoint_and_cookie_raw(state, &location)?;
        let mut request = state
            .request_client
            .head(head_url)
            .header("Tus-Resumable", "1.0.0");
        if let Some(cookie) = cookie {
            request = request.header(COOKIE, cookie);
        }
        match request.send().await {
            Ok(response) if response.status().is_success() => {
                offset = response
                    .headers()
                    .get("Upload-Offset")
                    .and_then(|value| value.to_str().ok())
                    .and_then(|value| value.parse().ok())
                    .unwrap_or(0);
            }
            _ => location.clear(),
        }
    }
    if offset > size {
        offset = 0;
        location.clear();
    }

    if location.is_empty() {
        let (url, cookie) = endpoint_and_cookie_raw(state, "/api/uploads")?;
        let mime_type = mime_guess::from_path(path)
            .first_or_octet_stream()
            .to_string();
        let upload_metadata = [
            format!("filename {}", STANDARD.encode(file_name.as_bytes())),
            format!("filetype {}", STANDARD.encode(mime_type.as_bytes())),
            format!(
                "lastmodified {}",
                STANDARD.encode(modified.to_string().as_bytes())
            ),
            format!(
                "conversation {}",
                STANDARD.encode(conversation_id.as_bytes())
            ),
        ]
        .join(",");
        let mut request = state
            .request_client
            .post(url)
            .header("Tus-Resumable", "1.0.0")
            .header("Upload-Length", size)
            .header("Upload-Metadata", upload_metadata);
        if let Some(cookie) = cookie {
            request = request.header(COOKIE, cookie);
        }
        let response = request.send().await.map_err(network_error)?;
        if !response.status().is_success() {
            return Err(response_error(response).await);
        }
        location = response
            .headers()
            .get("Location")
            .and_then(|value| value.to_str().ok())
            .unwrap_or_default()
            .to_owned();
        if !location.starts_with("/api/uploads/") {
            return Err("服务器返回了无效的上传地址".into());
        }
        update_upload_location(app, state, &fingerprint, Some(location.clone()))?;
    }

    emit_upload_progress(
        app,
        &file_name,
        if size == 0 {
            1.0
        } else {
            offset as f64 / size as f64
        },
    );
    let mut input = tokio::fs::File::open(path)
        .await
        .map_err(|error| format!("无法打开文件：{error}"))?;
    input
        .seek(std::io::SeekFrom::Start(offset))
        .await
        .map_err(|error| format!("无法恢复上传位置：{error}"))?;
    let mut buffer = vec![0_u8; CHUNK_SIZE];
    while offset < size {
        let requested =
            usize::try_from((size - offset).min(CHUNK_SIZE as u64)).unwrap_or(CHUNK_SIZE);
        input
            .read_exact(&mut buffer[..requested])
            .await
            .map_err(|error| format!("读取文件失败：{error}"))?;
        let (patch_url, cookie) = endpoint_and_cookie_raw(state, &location)?;
        let mut request = state
            .request_client
            .patch(patch_url)
            .header("Tus-Resumable", "1.0.0")
            .header("Content-Type", "application/offset+octet-stream")
            .header("Upload-Offset", offset)
            .body(buffer[..requested].to_vec());
        if let Some(cookie) = cookie {
            request = request.header(COOKIE, cookie);
        }
        let response = request.send().await.map_err(network_error)?;
        if !response.status().is_success() {
            return Err(response_error(response).await);
        }
        offset = response
            .headers()
            .get("Upload-Offset")
            .and_then(|value| value.to_str().ok())
            .and_then(|value| value.parse().ok())
            .unwrap_or(offset + requested as u64);
        emit_upload_progress(
            app,
            &file_name,
            if size == 0 {
                1.0
            } else {
                offset as f64 / size as f64
            },
        );
    }
    update_upload_location(app, state, &fingerprint, None)?;
    Ok(())
}

fn emit_upload_progress(app: &AppHandle, file_name: &str, fraction: f64) {
    let _ = app.emit(
        "upload-progress",
        UploadProgress {
            file_name: file_name.to_owned(),
            fraction,
        },
    );
}

fn upload_fingerprint(
    state: &AppState,
    path: &Path,
    conversation_id: &str,
    size: u64,
    modified: u128,
) -> Result<String, String> {
    let base = state
        .runtime
        .read()
        .map_err(|_| "客户端状态不可用")?
        .settings
        .base_url
        .clone()
        .ok_or("请先连接服务器")?;
    let value = format!(
        "{base}|{conversation_id}|{}|{size}|{modified}",
        path.display()
    );
    Ok(format!("{:x}", Sha256::digest(value.as_bytes())))
}

fn update_upload_location(
    app: &AppHandle,
    state: &AppState,
    key: &str,
    location: Option<String>,
) -> Result<(), String> {
    {
        let mut runtime = state.runtime.write().map_err(|_| "客户端状态不可用")?;
        if let Some(location) = location {
            runtime.settings.uploads.insert(key.to_owned(), location);
        } else {
            runtime.settings.uploads.remove(key);
        }
    }
    save_current_settings_raw(app, state)
}

fn endpoint_and_cookie(
    state: &State<'_, AppState>,
    path: &str,
) -> Result<(Url, Option<String>), String> {
    endpoint_and_cookie_raw(state, path)
}

fn endpoint_and_cookie_raw(state: &AppState, path: &str) -> Result<(Url, Option<String>), String> {
    if !path.starts_with("/api/") || path.contains("\\") {
        return Err("请求地址无效".into());
    }
    let runtime = state.runtime.read().map_err(|_| "客户端状态不可用")?;
    let base = runtime.settings.base_url.as_ref().ok_or("请先连接服务器")?;
    let base = Url::parse(base).map_err(|_| "服务器地址无效")?;
    let url = base.join(path).map_err(|_| "请求地址无效")?;
    if url.origin() != base.origin() {
        return Err("请求地址越界".into());
    }
    Ok((url, runtime.cookie.clone()))
}

async fn json_response(response: Response) -> Result<Value, String> {
    if !response.status().is_success() {
        return Err(response_error(response).await);
    }
    if response.status() == StatusCode::NO_CONTENT {
        return Ok(Value::Null);
    }
    let bytes = response.bytes().await.map_err(network_error)?;
    if bytes.is_empty() {
        return Ok(Value::Null);
    }
    serde_json::from_slice(&bytes).map_err(|_| "服务器返回了无法识别的响应".into())
}

async fn response_error(response: Response) -> String {
    let status = response.status();
    let body = response.text().await.unwrap_or_default();
    serde_json::from_str::<Value>(&body)
        .ok()
        .and_then(|value| {
            value
                .get("error")
                .and_then(Value::as_str)
                .map(str::to_owned)
        })
        .unwrap_or_else(|| format!("请求失败 ({})", status.as_u16()))
}

fn network_error(error: reqwest::Error) -> String {
    if error.is_timeout() {
        "连接服务器超时".into()
    } else {
        format!("无法连接服务器：{error}")
    }
}

fn capture_session_cookie(
    app: &AppHandle,
    state: &AppState,
    headers: &HeaderMap,
) -> Result<bool, String> {
    let session = headers
        .get_all(SET_COOKIE)
        .iter()
        .filter_map(|value| value.to_str().ok())
        .filter_map(|value| value.split(';').next())
        .find(|value| value.starts_with("selfsend_session"))
        .map(str::to_owned);
    let Some(cookie) = session else {
        return Ok(false);
    };
    let base_url = {
        let mut runtime = state.runtime.write().map_err(|_| "客户端状态不可用")?;
        let base = runtime.settings.base_url.clone().ok_or("请先连接服务器")?;
        runtime.cookie = if cookie.ends_with('=') {
            None
        } else {
            Some(cookie.clone())
        };
        base
    };
    if cookie.ends_with('=') {
        delete_cookie(&base_url);
    } else {
        save_cookie(&base_url, &cookie)?;
    }
    let _ = app;
    Ok(true)
}

fn normalize_server_url(value: &str) -> Result<String, String> {
    let mut raw = value.trim().to_owned();
    if !raw.contains("://") {
        raw = format!("http://{raw}");
    }
    let mut url = Url::parse(&raw).map_err(|_| "服务器地址无效")?;
    if url.scheme() != "http" && url.scheme() != "https" {
        return Err("只支持 HTTP 或 HTTPS 地址".into());
    }
    if !url.username().is_empty() || url.password().is_some() {
        return Err("服务器地址不能包含用户名或密码".into());
    }
    if url.host().is_none() {
        return Err("服务器地址无效".into());
    }
    if url.scheme() == "http" && !is_private_host(url.host().expect("host checked")) {
        return Err("公网服务器必须使用 HTTPS 地址".into());
    }
    url.set_path("");
    url.set_query(None);
    url.set_fragment(None);
    Ok(url.as_str().trim_end_matches('/').to_owned())
}

fn is_private_host(host: Host<&str>) -> bool {
    match host {
        Host::Ipv4(ip) => ip.is_private() || ip.is_loopback() || ip.is_link_local(),
        Host::Ipv6(ip) => {
            ip.is_loopback() || ip.is_unicast_link_local() || (ip.segments()[0] & 0xfe00) == 0xfc00
        }
        Host::Domain(name) => {
            let name = name.to_ascii_lowercase();
            name == "localhost"
                || name.ends_with(".local")
                || name.ends_with(".home")
                || name.ends_with(".lan")
                || name
                    .parse::<IpAddr>()
                    .map(|ip| match ip {
                        IpAddr::V4(ip) => ip.is_private() || ip.is_loopback() || ip.is_link_local(),
                        IpAddr::V6(ip) => ip.is_loopback() || ip.is_unicast_link_local(),
                    })
                    .unwrap_or(false)
        }
    }
}

fn credential_key(base_url: &str) -> String {
    format!("{:x}", Sha256::digest(base_url.as_bytes()))
}

fn credential_entry(base_url: &str) -> Option<keyring::Entry> {
    keyring::Entry::new(CREDENTIAL_SERVICE, &credential_key(base_url)).ok()
}

fn load_cookie(base_url: &str) -> Option<String> {
    credential_entry(base_url)?
        .get_password()
        .ok()
        .filter(|value| value.starts_with("selfsend_session"))
}

fn save_cookie(base_url: &str, cookie: &str) -> Result<(), String> {
    credential_entry(base_url)
        .ok_or("无法访问 Windows 凭据管理器")?
        .set_password(cookie)
        .map_err(|error| format!("无法保存登录凭据：{error}"))
}

fn delete_cookie(base_url: &str) {
    if let Some(entry) = credential_entry(base_url) {
        let _ = entry.delete_credential();
    }
}

fn settings_path(app: &AppHandle) -> Result<PathBuf, String> {
    Ok(app
        .path()
        .app_config_dir()
        .map_err(|error| format!("无法定位设置目录：{error}"))?
        .join("settings.json"))
}

fn load_settings(app: &AppHandle) -> DesktopSettings {
    settings_path(app)
        .ok()
        .and_then(|path| std::fs::read(path).ok())
        .and_then(|bytes| serde_json::from_slice(&bytes).ok())
        .unwrap_or_default()
}

fn save_current_settings(app: &AppHandle, state: &State<'_, AppState>) -> Result<(), String> {
    save_current_settings_raw(app, state)
}

fn save_current_settings_raw(app: &AppHandle, state: &AppState) -> Result<(), String> {
    let settings = state
        .runtime
        .read()
        .map_err(|_| "客户端状态不可用")?
        .settings
        .clone();
    let path = settings_path(app)?;
    if let Some(parent) = path.parent() {
        std::fs::create_dir_all(parent).map_err(|error| format!("无法创建设置目录：{error}"))?;
    }
    let bytes =
        serde_json::to_vec_pretty(&settings).map_err(|error| format!("无法序列化设置：{error}"))?;
    std::fs::write(path, bytes).map_err(|error| format!("无法保存设置：{error}"))
}

fn start_event_monitor(app: AppHandle) {
    let generation = {
        let state = app.state::<AppState>();
        state.monitor_generation.fetch_add(1, Ordering::SeqCst) + 1
    };
    tauri::async_runtime::spawn(async move {
        loop {
            let (url, cookie, client, current_generation) = {
                let state = app.state::<AppState>();
                let current_generation = state.monitor_generation.load(Ordering::SeqCst);
                let runtime = match state.runtime.read() {
                    Ok(value) => value,
                    Err(_) => return,
                };
                let url = runtime
                    .settings
                    .base_url
                    .as_ref()
                    .and_then(|base| Url::parse(base).ok())
                    .and_then(|base| base.join("/api/events").ok());
                (
                    url,
                    runtime.cookie.clone(),
                    state.stream_client.clone(),
                    current_generation,
                )
            };
            if current_generation != generation {
                return;
            }
            let (Some(url), Some(cookie)) = (url, cookie) else {
                tokio::time::sleep(Duration::from_secs(10)).await;
                continue;
            };
            let response = client.get(url).header(COOKIE, cookie).send().await;
            let Ok(response) = response else {
                tokio::time::sleep(Duration::from_secs(5)).await;
                continue;
            };
            if !response.status().is_success() {
                tokio::time::sleep(Duration::from_secs(10)).await;
                continue;
            }
            let mut stream = response.bytes_stream();
            let mut pending = String::new();
            while let Some(chunk) = stream.next().await {
                if app
                    .state::<AppState>()
                    .monitor_generation
                    .load(Ordering::SeqCst)
                    != generation
                {
                    return;
                }
                let Ok(chunk) = chunk else {
                    break;
                };
                pending.push_str(&String::from_utf8_lossy(&chunk).replace("\r\n", "\n"));
                while let Some(end) = pending.find("\n\n") {
                    let packet = pending[..end].to_owned();
                    pending.drain(..end + 2);
                    if let Some(kind) = packet.lines().find_map(|line| line.strip_prefix("event: "))
                    {
                        handle_server_event(&app, kind);
                    }
                }
            }
            tokio::time::sleep(Duration::from_secs(3)).await;
        }
    });
}

fn handle_server_event(app: &AppHandle, kind: &str) {
    let _ = app.emit(
        "server-event",
        ServerEvent {
            kind: kind.to_owned(),
        },
    );
    if kind != "timeline" {
        return;
    }
    let hidden = app
        .get_webview_window("main")
        .map(|window| {
            !window.is_visible().unwrap_or(false) || !window.is_focused().unwrap_or(false)
        })
        .unwrap_or(true);
    if hidden {
        let _ = app
            .notification()
            .builder()
            .title("SelfSend")
            .body("收到一条新消息或文件")
            .show();
    }
}

fn show_main_window(app: &AppHandle) -> tauri::Result<()> {
    if let Some(window) = app.get_webview_window("main") {
        let _ = window.unminimize();
        let _ = window.show();
        let _ = window.set_focus();
        return Ok(());
    }
    WebviewWindowBuilder::new(app, "main", WebviewUrl::App("index.html".into()))
        .title("SelfSend")
        .inner_size(920.0, 680.0)
        .min_inner_size(430.0, 560.0)
        .center()
        .build()?;
    Ok(())
}

fn setup_tray(app: &tauri::App) -> tauri::Result<()> {
    let open = MenuItem::with_id(app, "open", "打开 SelfSend", true, None::<&str>)?;
    let quit = MenuItem::with_id(app, "quit", "退出", true, None::<&str>)?;
    let menu = Menu::with_items(app, &[&open, &quit])?;
    let mut tray = TrayIconBuilder::with_id("selfsend-tray")
        .tooltip("SelfSend")
        .menu(&menu)
        .show_menu_on_left_click(false)
        .on_menu_event(|app, event| match event.id.as_ref() {
            "open" => {
                let _ = show_main_window(app);
            }
            "quit" => app.exit(0),
            _ => {}
        })
        .on_tray_icon_event(|tray, event| {
            if matches!(
                event,
                TrayIconEvent::Click {
                    button: MouseButton::Left,
                    button_state: MouseButtonState::Up,
                    ..
                }
            ) {
                let _ = show_main_window(tray.app_handle());
            }
        });
    if let Some(icon) = app.default_window_icon() {
        tray = tray.icon(icon.clone());
    }
    tray.build(app)?;
    Ok(())
}

pub fn run() {
    tauri::Builder::default()
        .plugin(tauri_plugin_single_instance::init(|app, _args, _cwd| {
            let _ = show_main_window(app);
        }))
        .plugin(tauri_plugin_autostart::init(
            MacosLauncher::LaunchAgent,
            Some(vec!["--background"]),
        ))
        .plugin(tauri_plugin_notification::init())
        .plugin(tauri_plugin_dialog::init())
        .manage(AppState::new())
        .setup(|app| {
            let settings = load_settings(app.handle());
            let cookie = settings.base_url.as_deref().and_then(load_cookie);
            {
                let state = app.state::<AppState>();
                let mut runtime = state.runtime.write().expect("runtime state");
                runtime.settings = settings;
                runtime.cookie = cookie;
            }
            setup_tray(app)?;

            let initialize_autostart = !app
                .state::<AppState>()
                .runtime
                .read()
                .expect("runtime state")
                .settings
                .autostart_initialized;
            if initialize_autostart {
                let _ = app.autolaunch().enable();
                {
                    let state = app.state::<AppState>();
                    state
                        .runtime
                        .write()
                        .expect("runtime state")
                        .settings
                        .autostart_initialized = true;
                    let _ = save_current_settings_raw(app.handle(), &state);
                }
            }
            start_event_monitor(app.handle().clone());
            let background = std::env::args_os().any(|argument| argument == "--background");
            if !background {
                show_main_window(app.handle())?;
            }
            Ok(())
        })
        .on_window_event(|window, event| {
            if window.label() == "main" {
                if let tauri::WindowEvent::CloseRequested { api, .. } = event {
                    api.prevent_close();
                    let _ = window.hide();
                }
            }
        })
        .invoke_handler(tauri::generate_handler![
            desktop_bootstrap,
            configure_server,
            clear_server,
            autostart_enabled,
            set_autostart,
            api_request,
            upload_files,
            download_file,
        ])
        .run(tauri::generate_context!())
        .expect("error while running SelfSend");
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn normalizes_local_server_addresses() {
        assert_eq!(
            normalize_server_url(" 192.168.1.20:8080/path?x=1 ").unwrap(),
            "http://192.168.1.20:8080"
        );
        assert_eq!(
            normalize_server_url("http://selfsend.local:8080/").unwrap(),
            "http://selfsend.local:8080"
        );
    }

    #[test]
    fn requires_https_for_public_servers() {
        assert!(normalize_server_url("http://selfsend.example.com").is_err());
        assert_eq!(
            normalize_server_url("https://selfsend.example.com/path").unwrap(),
            "https://selfsend.example.com"
        );
    }

    #[test]
    fn rejects_credentials_and_non_http_schemes() {
        assert!(normalize_server_url("ftp://192.168.1.20").is_err());
        assert!(normalize_server_url("http://user:password@192.168.1.20").is_err());
    }
}
