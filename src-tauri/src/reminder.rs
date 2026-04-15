use crate::state::{Kind, SharedState};
use parking_lot::Mutex;
use std::collections::HashMap;
use std::sync::mpsc;
use std::sync::Arc;
use tauri::{AppHandle, Manager, WebviewUrl, WebviewWindowBuilder, WindowEvent};

pub type InputResults = Arc<Mutex<HashMap<String, mpsc::Sender<Option<String>>>>>;

fn encode(s: &str) -> String {
    urlencoding::encode(s).into_owned()
}

fn unique_label() -> String {
    use std::time::{SystemTime, UNIX_EPOCH};
    let ns = SystemTime::now()
        .duration_since(UNIX_EPOCH)
        .map(|d| d.as_nanos())
        .unwrap_or(0);
    format!("dialog-{}", ns)
}

fn build(
    app: &AppHandle,
    title: &str,
    body: &str,
    btn: &str,
    timeout_secs: Option<u32>,
    height: f64,
) -> tauri::Result<tauri::WebviewWindow> {
    let timeout_str = timeout_secs.map(|t| t.to_string()).unwrap_or_default();
    let url = format!(
        "dialog.html#title={}&body={}&btn={}&timeout={}",
        encode(title),
        encode(body),
        encode(btn),
        encode(&timeout_str),
    );
    let label = unique_label();
    WebviewWindowBuilder::new(app, &label, WebviewUrl::App(url.into()))
        .title(title)
        .inner_size(460.0, height)
        .decorations(false)
        .transparent(true)
        .always_on_top(true)
        .skip_taskbar(true)
        .resizable(false)
        .center()
        .shadow(true)
        .focused(true)
        .build()
}

/// 阻塞式提醒：弹窗 → 用户点按钮（或 Esc / 关闭）→ 计数
pub fn show_blocking(app: &AppHandle, state: SharedState, kind: Kind, title: &str, body: &str, btn: &str) {
    let win = match build(app, title, body, btn, None, 220.0) {
        Ok(w) => w,
        Err(_) => {
            state.lock().confirm(kind);
            return;
        }
    };

    let (tx, rx) = mpsc::channel::<()>();
    let tx_clone = tx.clone();
    win.on_window_event(move |e| {
        if matches!(e, WindowEvent::Destroyed | WindowEvent::CloseRequested { .. }) {
            let _ = tx_clone.send(());
        }
    });

    let _ = rx.recv();
    state.lock().confirm(kind);
}

/// 非阻塞：立即计数，弹窗自己定时关闭
pub fn show_timed(app: &AppHandle, title: &str, body: &str, timeout_secs: u32) {
    let _ = build(app, title, body, "知道了", Some(timeout_secs), 220.0);
}

pub fn dispatch_reminder(app: AppHandle, state: SharedState, kind: Kind) {
    std::thread::spawn(move || match kind {
        Kind::Eye => {
            state.lock().confirm(Kind::Eye);
            show_timed(&app, "👀 该休息眼睛了", "看看 6 米外的远处，持续 20 秒，放松一下眼睛。", 6);
        }
        Kind::Water => {
            show_blocking(&app, state, Kind::Water, "💧 喝水提醒", "该喝水了！起来倒杯水吧。", "喝水了");
        }
        Kind::Stand => {
            show_blocking(&app, state, Kind::Stand, "🧍 站立提醒", "该站起来了！走走伸展一下。", "站立了");
        }
    });
}

pub fn show_lunch(app: &AppHandle) {
    show_timed(app, "🍚 该吃午饭了", "别忘了吃午饭！按时吃饭，身体才能保持好状态。", 8);
}

/// 弹出输入框，阻塞直到提交或取消。validate_kind: "" / "hhmm"
pub fn show_input(
    app: &AppHandle,
    title: &str,
    prompt: &str,
    default: &str,
    validate_kind: &str,
) -> Option<String> {
    let label = unique_label();
    let (tx, rx) = mpsc::channel::<Option<String>>();
    let results = app.state::<InputResults>().inner().clone();
    results.lock().insert(label.clone(), tx);

    let url = format!(
        "input.html#title={}&prompt={}&default={}&label={}&validate={}",
        encode(title),
        encode(prompt),
        encode(default),
        encode(&label),
        encode(validate_kind),
    );

    let win = match WebviewWindowBuilder::new(app, &label, WebviewUrl::App(url.into()))
        .title(title)
        .inner_size(460.0, 260.0)
        .decorations(false)
        .transparent(true)
        .always_on_top(true)
        .skip_taskbar(true)
        .resizable(false)
        .center()
        .shadow(true)
        .focused(true)
        .visible(true)
        .build()
    {
        Ok(w) => w,
        Err(_) => {
            results.lock().remove(&label);
            return None;
        }
    };

    // macOS: Accessory 模式下窗口默认不会被激活到前台，临时切换并主动激活
    #[cfg(target_os = "macos")]
    {
        let _ = app.set_activation_policy(tauri::ActivationPolicy::Regular);
    }
    let _ = win.show();
    let _ = win.set_focus();
    let _ = win.set_always_on_top(true);

    let label_clone = label.clone();
    let results_clone = results.clone();
    let app_for_close = app.clone();
    win.on_window_event(move |e| {
        if matches!(e, WindowEvent::Destroyed) {
            results_clone.lock().remove(&label_clone);
            #[cfg(target_os = "macos")]
            {
                let _ = app_for_close.set_activation_policy(tauri::ActivationPolicy::Accessory);
            }
            #[cfg(not(target_os = "macos"))]
            { let _ = &app_for_close; }
        }
    });

    rx.recv().ok().flatten()
}

pub fn show_off_work(app: &AppHandle, work_minutes: u32) {
    let body = format!(
        "今天已工作 {} 小时 {} 分钟，辛苦了！该休息了。",
        work_minutes / 60,
        work_minutes % 60
    );
    show_timed(app, "🏠 该下班了", &body, 10);
}
