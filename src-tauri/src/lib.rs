mod holidays;
mod icon;
mod menu;
mod reminder;
mod state;
mod storage;

use crate::menu::MenuRefs;
use crate::reminder::InputResults;
use crate::state::{AppState, Kind, SharedState};
use chrono::Timelike;
use parking_lot::Mutex;
use std::collections::HashMap;
use std::sync::Arc;
use std::time::Duration;
use tauri::tray::TrayIconBuilder;
use tauri::Manager;

type SharedMenu = Arc<Mutex<Option<MenuRefs>>>;

#[tauri::command]
fn submit_input(label: String, value: Option<String>, results: tauri::State<'_, InputResults>) {
    if let Some(tx) = results.lock().remove(&label) {
        let _ = tx.send(value);
    }
}

#[tauri::command]
fn close_dialog(label: String, app: tauri::AppHandle) {
    if let Some(w) = app.get_webview_window(&label) {
        let _ = w.close();
    }
}

#[cfg_attr(mobile, tauri::mobile_entry_point)]
pub fn run() {
    let shared_state: SharedState = Arc::new(Mutex::new(AppState::new()));
    let shared_menu: SharedMenu = Arc::new(Mutex::new(None));
    let input_results: InputResults = Arc::new(Mutex::new(HashMap::new()));

    tauri::Builder::default()
        .manage(shared_state.clone())
        .manage(shared_menu.clone())
        .manage(input_results.clone())
        .invoke_handler(tauri::generate_handler![submit_input, close_dialog])
        .setup(move |app| {
            #[cfg(target_os = "macos")]
            app.set_activation_policy(tauri::ActivationPolicy::Accessory);

            let handle = app.handle();

            let (menu, refs) = menu::build(handle, &shared_state)?;
            *shared_menu.lock() = Some(refs);

            let state_for_menu = shared_state.clone();
            let menu_for_events = shared_menu.clone();
            let _tray = TrayIconBuilder::with_id("main")
                .tooltip("健康提醒小助手")
                .icon(icon::water_drop())
                .icon_as_template(false)
                .menu(&menu)
                .show_menu_on_left_click(true)
                .on_menu_event(move |app, event| {
                    handle_menu_event(app, &state_for_menu, &menu_for_events, event.id.as_ref());
                })
                .build(handle)?;

            let state_clone = shared_state.clone();
            std::thread::spawn(move || holidays::sync(state_clone));

            let app_handle = handle.clone();
            let tick_state = shared_state.clone();
            let tick_menu = shared_menu.clone();
            std::thread::spawn(move || tick_loop(app_handle, tick_state, tick_menu));

            Ok(())
        })
        .run(tauri::generate_context!())
        .expect("error while running tauri application");
}

fn handle_menu_event(
    app: &tauri::AppHandle,
    state: &SharedState,
    menu_refs: &SharedMenu,
    id: &str,
) {
    match id {
        "quit" => {
            {
                let s = state.lock();
                s.save_history_snapshot();
                storage::save_config(&s.cfg);
            }
            app.exit(0);
        }
        "record_water" => state.lock().confirm(Kind::Water),
        "record_stand" => state.lock().confirm(Kind::Stand),
        "pause" => state.lock().toggle_pause(),
        "history" => {
            let p = storage::data_dir().join("history.json");
            let _ = open_file(&p);
        }
        "lunch_set" => {
            spawn_time_input(app.clone(), state.clone(), menu_refs.clone(), true);
        }
        "off_work_set" => {
            spawn_time_input(app.clone(), state.clone(), menu_refs.clone(), false);
        }
        other if other.starts_with("water_int_") => {
            if let Ok(m) = other.trim_start_matches("water_int_").parse::<u32>() {
                state.lock().set_interval(Kind::Water, m);
                sync_check(menu_refs, Kind::Water, m);
            }
        }
        other if other.starts_with("stand_int_") => {
            if let Ok(m) = other.trim_start_matches("stand_int_").parse::<u32>() {
                state.lock().set_interval(Kind::Stand, m);
                sync_check(menu_refs, Kind::Stand, m);
            }
        }
        other if other.starts_with("eye_int_") => {
            if let Ok(m) = other.trim_start_matches("eye_int_").parse::<u32>() {
                state.lock().set_interval(Kind::Eye, m);
                sync_check(menu_refs, Kind::Eye, m);
            }
        }
        _ => {}
    }
}

fn spawn_time_input(app: tauri::AppHandle, state: SharedState, menu_refs: SharedMenu, lunch: bool) {
    std::thread::spawn(move || {
        let (title, prompt, default) = {
            let s = state.lock();
            if lunch {
                ("设置午饭提醒".to_string(), "请输入午饭提醒时间（HH:MM）".to_string(), s.cfg.lunch_time.clone())
            } else {
                ("设置下班提醒".to_string(), "请输入下班提醒时间（HH:MM）".to_string(), s.cfg.off_work.clone())
            }
        };
        let result = reminder::show_input(&app, &title, &prompt, &default, "hhmm");
        if let Some(value) = result {
            let mut s = state.lock();
            if lunch {
                s.set_lunch(value.clone());
            } else {
                s.set_off_work(value.clone());
            }
            drop(s);
            // 立即刷新对应菜单标题
            let refs = menu_refs.lock();
            if let Some(refs) = refs.as_ref() {
                if lunch {
                    let _ = refs.lunch_label.set_text(format!("午饭提醒:  {}  …", value));
                } else {
                    let _ = refs.off_work_label.set_text(format!("下班提醒:  {}  …", value));
                }
            }
        }
    });
}

fn sync_check(menu_refs: &SharedMenu, kind: Kind, mins: u32) {
    let refs = menu_refs.lock();
    let Some(refs) = refs.as_ref() else { return };
    let (items, values) = match kind {
        Kind::Water => (&refs.water_int, menu::WATER_INT),
        Kind::Stand => (&refs.stand_int, menu::STAND_INT),
        Kind::Eye => (&refs.eye_int, menu::EYE_INT),
    };
    for (it, &v) in items.iter().zip(values.iter()) {
        let _ = it.set_checked(v == mins);
    }
}

fn open_file(path: &std::path::Path) -> std::io::Result<()> {
    #[cfg(target_os = "macos")]
    {
        std::process::Command::new("open").arg(path).spawn()?;
    }
    #[cfg(target_os = "windows")]
    {
        std::process::Command::new("cmd")
            .args(["/c", "start", "", &path.to_string_lossy()])
            .spawn()?;
    }
    Ok(())
}

fn tick_loop(app: tauri::AppHandle, state: SharedState, menu_refs: SharedMenu) {
    loop {
        std::thread::sleep(Duration::from_secs(1));
        let triggers = {
            let mut s = state.lock();
            s.check_rollover();

            let mut triggers: Vec<Kind> = Vec::new();
            let mut lunch_fire = false;
            let mut off_work_fire = false;

            if !s.paused {
                if !s.water_state.waiting {
                    if let Some(last) = s.water_state.last_action {
                        if last.elapsed() >= Duration::from_secs(s.cfg.water_min as u64 * 60) {
                            s.water_state.waiting = true;
                            triggers.push(Kind::Water);
                        }
                    }
                }
                if !s.stand_state.waiting {
                    if let Some(last) = s.stand_state.last_action {
                        if last.elapsed() >= Duration::from_secs(s.cfg.stand_min as u64 * 60) {
                            s.stand_state.waiting = true;
                            triggers.push(Kind::Stand);
                        }
                    }
                }
                if !s.eye_state.waiting {
                    if let Some(last) = s.eye_state.last_action {
                        if last.elapsed() >= Duration::from_secs(s.cfg.eye_min as u64 * 60) {
                            s.eye_state.waiting = true;
                            triggers.push(Kind::Eye);
                        }
                    }
                }

                let now = chrono::Local::now();
                let mins_now = (now.hour() * 60 + now.minute()) as u32;
                let lunch_mins = state::parse_hhmm_minutes(&s.cfg.lunch_time);
                if !s.lunch_done && mins_now >= lunch_mins && mins_now < lunch_mins + 30 {
                    s.lunch_done = true;
                    lunch_fire = true;
                }
                let off_mins = state::parse_hhmm_minutes(&s.cfg.off_work);
                if !s.off_work_done && s.is_workday() && mins_now >= off_mins && mins_now < off_mins + 30 {
                    s.off_work_done = true;
                    off_work_fire = true;
                }
            }

            drop(s);

            if lunch_fire {
                let app_c = app.clone();
                std::thread::spawn(move || reminder::show_lunch(&app_c));
            }
            if off_work_fire {
                let work_min = state.lock().work_start.elapsed().as_secs() / 60;
                let app_c = app.clone();
                std::thread::spawn(move || reminder::show_off_work(&app_c, work_min as u32));
            }

            triggers
        };

        for kind in triggers {
            reminder::dispatch_reminder(app.clone(), state.clone(), kind);
        }

        update_menu_titles(&state, &menu_refs);
    }
}

fn update_menu_titles(state: &SharedState, menu_refs: &SharedMenu) {
    let s = state.lock();
    let refs = menu_refs.lock();
    let Some(refs) = refs.as_ref() else { return };

    let paused = s.paused;
    let w_elapsed = s.water_state.last_action.map(|t| t.elapsed().as_secs());
    let st_elapsed = s.stand_state.last_action.map(|t| t.elapsed().as_secs());
    let e_elapsed = s.eye_state.last_action.map(|t| t.elapsed().as_secs());

    let _ = refs.water_timer.set_text(format!("下次喝水:  {}", menu::fmt_remaining(s.cfg.water_min, w_elapsed, paused, s.water_state.waiting)));
    let _ = refs.stand_timer.set_text(format!("下次站立:  {}", menu::fmt_remaining(s.cfg.stand_min, st_elapsed, paused, s.stand_state.waiting)));
    let _ = refs.eye_timer.set_text(format!("下次护眼:  {}", menu::fmt_remaining(s.cfg.eye_min, e_elapsed, paused, s.eye_state.waiting)));

    let work_secs = s.work_start.elapsed().as_secs();
    let wm = work_secs / 60;
    let day = if s.is_workday() { "工作日" } else { "休息日" };
    let _ = refs.work_time.set_text(format!("今日工作:  {}h{}m · {}", wm / 60, wm % 60, day));
    let _ = refs.stats.set_text(format!("今日统计:  喝水{} · 站立{} · 护眼{}", s.water, s.stand, s.eye));

    let _ = refs.pause.set_text(if paused { "继续提醒" } else { "暂停提醒" });
}
