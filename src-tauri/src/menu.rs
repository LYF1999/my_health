use crate::state::SharedState;
use tauri::menu::{CheckMenuItem, Menu, MenuItem, PredefinedMenuItem, Submenu};
use tauri::{AppHandle, Wry};

pub const WATER_INT: &[u32] = &[15, 20, 30, 45, 60];
pub const STAND_INT: &[u32] = &[20, 30, 45, 60, 90];
pub const EYE_INT: &[u32] = &[10, 15, 20, 25, 30];

pub struct MenuRefs {
    pub water_timer: MenuItem<Wry>,
    pub stand_timer: MenuItem<Wry>,
    pub eye_timer: MenuItem<Wry>,
    pub work_time: MenuItem<Wry>,
    pub stats: MenuItem<Wry>,
    pub pause: MenuItem<Wry>,
    pub water_int: Vec<CheckMenuItem<Wry>>,
    pub stand_int: Vec<CheckMenuItem<Wry>>,
    pub eye_int: Vec<CheckMenuItem<Wry>>,
    pub lunch_label: MenuItem<Wry>,
    pub off_work_label: MenuItem<Wry>,
}

pub fn build(app: &AppHandle, state: &SharedState) -> tauri::Result<(Menu<Wry>, MenuRefs)> {
    let s = state.lock();

    let title = MenuItem::with_id(app, "title", "健康提醒小助手", false, None::<&str>)?;

    let water_timer = MenuItem::with_id(app, "water_timer", "下次喝水:  --:--", false, None::<&str>)?;
    let stand_timer = MenuItem::with_id(app, "stand_timer", "下次站立:  --:--", false, None::<&str>)?;
    let eye_timer = MenuItem::with_id(app, "eye_timer", "下次护眼:  --:--", false, None::<&str>)?;

    let work_time = MenuItem::with_id(app, "work_time", "今日工作:  0h0m", false, None::<&str>)?;
    let stats = MenuItem::with_id(app, "stats", "今日统计:  0/0/0", false, None::<&str>)?;

    let record_water = MenuItem::with_id(app, "record_water", "记录喝水", true, None::<&str>)?;
    let record_stand = MenuItem::with_id(app, "record_stand", "记录站立", true, None::<&str>)?;

    let mut water_int = Vec::new();
    for &m in WATER_INT {
        water_int.push(CheckMenuItem::with_id(
            app,
            format!("water_int_{}", m),
            format!("{} 分钟", m),
            true,
            m == s.cfg.water_min,
            None::<&str>,
        )?);
    }
    let water_sub = Submenu::with_items(
        app,
        "喝水间隔",
        true,
        &water_int.iter().map(|i| i as &dyn tauri::menu::IsMenuItem<Wry>).collect::<Vec<_>>(),
    )?;

    let mut stand_int = Vec::new();
    for &m in STAND_INT {
        stand_int.push(CheckMenuItem::with_id(
            app,
            format!("stand_int_{}", m),
            format!("{} 分钟", m),
            true,
            m == s.cfg.stand_min,
            None::<&str>,
        )?);
    }
    let stand_sub = Submenu::with_items(
        app,
        "站立间隔",
        true,
        &stand_int.iter().map(|i| i as &dyn tauri::menu::IsMenuItem<Wry>).collect::<Vec<_>>(),
    )?;

    let mut eye_int = Vec::new();
    for &m in EYE_INT {
        eye_int.push(CheckMenuItem::with_id(
            app,
            format!("eye_int_{}", m),
            format!("{} 分钟", m),
            true,
            m == s.cfg.eye_min,
            None::<&str>,
        )?);
    }
    let eye_sub = Submenu::with_items(
        app,
        "护眼间隔",
        true,
        &eye_int.iter().map(|i| i as &dyn tauri::menu::IsMenuItem<Wry>).collect::<Vec<_>>(),
    )?;

    let lunch_label = MenuItem::with_id(
        app,
        "lunch_set",
        format!("午饭提醒:  {}  …", s.cfg.lunch_time),
        true,
        None::<&str>,
    )?;
    let off_work_label = MenuItem::with_id(
        app,
        "off_work_set",
        format!("下班提醒:  {}  …", s.cfg.off_work),
        true,
        None::<&str>,
    )?;

    let pause = MenuItem::with_id(app, "pause", "暂停提醒", true, None::<&str>)?;
    let history = MenuItem::with_id(app, "history", "查看历史记录...", true, None::<&str>)?;
    let quit = MenuItem::with_id(app, "quit", "退出", true, None::<&str>)?;

    drop(s);

    let menu = Menu::with_items(
        app,
        &[
            &title,
            &PredefinedMenuItem::separator(app)?,
            &water_timer,
            &stand_timer,
            &eye_timer,
            &PredefinedMenuItem::separator(app)?,
            &work_time,
            &stats,
            &PredefinedMenuItem::separator(app)?,
            &record_water,
            &record_stand,
            &PredefinedMenuItem::separator(app)?,
            &water_sub,
            &stand_sub,
            &eye_sub,
            &lunch_label,
            &off_work_label,
            &PredefinedMenuItem::separator(app)?,
            &pause,
            &history,
            &PredefinedMenuItem::separator(app)?,
            &quit,
        ],
    )?;

    let refs = MenuRefs {
        water_timer,
        stand_timer,
        eye_timer,
        work_time,
        stats,
        pause,
        water_int,
        stand_int,
        eye_int,
        lunch_label,
        off_work_label,
    };

    Ok((menu, refs))
}

pub fn fmt_remaining(interval_min: u32, last_secs: Option<u64>, paused: bool, waiting: bool) -> String {
    if paused {
        return "已暂停".into();
    }
    if waiting {
        return "等待确认...".into();
    }
    let elapsed = last_secs.unwrap_or(0);
    let target = (interval_min as u64) * 60;
    let left = target.saturating_sub(elapsed);
    format!("{:02}:{:02}", left / 60, left % 60)
}
