use crate::state::SharedState;
use crate::storage;
use chrono::Datelike;
use serde::Deserialize;
use std::collections::HashMap;

#[derive(Deserialize)]
struct ApiResp {
    code: i32,
    holiday: HashMap<String, Entry>,
}

#[derive(Deserialize)]
struct Entry {
    holiday: bool,
    date: String,
}

pub fn sync(state: SharedState) {
    let year = chrono::Local::now().year();
    let url = format!("https://timor.tech/api/holiday/year/{}", year);
    let resp = match reqwest::blocking::get(&url) {
        Ok(r) => r,
        Err(_) => return,
    };
    let parsed: ApiResp = match resp.json() {
        Ok(p) => p,
        Err(_) => return,
    };
    if parsed.code != 0 {
        return;
    }

    let prefix = format!("{}-", year);
    let mut s = state.lock();

    let old_h: Vec<String> = s
        .holidays
        .holidays
        .iter()
        .filter(|d| !d.starts_with(&prefix))
        .cloned()
        .collect();
    let old_w: Vec<String> = s
        .holidays
        .workdays
        .iter()
        .filter(|d| !d.starts_with(&prefix))
        .cloned()
        .collect();

    let mut new_h = old_h;
    let mut new_w = old_w;
    for e in parsed.holiday.values() {
        if e.holiday {
            new_h.push(e.date.clone());
        } else {
            new_w.push(e.date.clone());
        }
    }
    s.holidays.holidays = new_h;
    s.holidays.workdays = new_w;
    storage::save_holidays(&s.holidays);
}
