use crate::storage::{self, Config, DailyRecord, HolidayConfig};
use chrono::{Datelike, Local, NaiveDate, Weekday};
use parking_lot::Mutex;
use std::sync::Arc;
use std::time::Instant;

#[derive(Default, Clone, Copy)]
pub struct Reminder {
    pub last_action: Option<Instant>,
    pub waiting: bool,
}

pub struct AppState {
    pub cfg: Config,
    pub paused: bool,
    pub water: u32,
    pub stand: u32,
    pub eye: u32,
    pub work_start: Instant,
    pub today: String,
    pub lunch_done: bool,
    pub off_work_done: bool,

    pub water_state: Reminder,
    pub stand_state: Reminder,
    pub eye_state: Reminder,

    pub holidays: HolidayConfig,
}

impl AppState {
    pub fn new() -> Self {
        let now = Instant::now();
        let today = Local::now().format("%Y-%m-%d").to_string();
        Self {
            cfg: storage::load_config(),
            paused: false,
            water: 0,
            stand: 0,
            eye: 0,
            work_start: now,
            today,
            lunch_done: false,
            off_work_done: false,
            water_state: Reminder { last_action: Some(now), waiting: false },
            stand_state: Reminder { last_action: Some(now), waiting: false },
            eye_state: Reminder { last_action: Some(now), waiting: false },
            holidays: storage::load_holidays(),
        }
    }

    pub fn save_history_snapshot(&self) {
        let work_min = self.work_start.elapsed().as_secs() / 60;
        let rec = DailyRecord {
            date: self.today.clone(),
            water: self.water,
            stand: self.stand,
            eye: self.eye,
            work_min: work_min as u32,
        };
        storage::upsert_today(rec);
    }

    pub fn is_workday(&self) -> bool {
        let today = Local::now().format("%Y-%m-%d").to_string();
        if self.holidays.workdays.iter().any(|d| d == &today) {
            return true;
        }
        if self.holidays.holidays.iter().any(|d| d == &today) {
            return false;
        }
        if self.holidays.skip_weekends {
            let wd = Local::now().weekday();
            if wd == Weekday::Sat || wd == Weekday::Sun {
                return false;
            }
        }
        true
    }

    pub fn confirm(&mut self, kind: Kind) {
        let now = Instant::now();
        match kind {
            Kind::Water => {
                self.water += 1;
                self.water_state.last_action = Some(now);
                self.water_state.waiting = false;
            }
            Kind::Stand => {
                self.stand += 1;
                self.stand_state.last_action = Some(now);
                self.stand_state.waiting = false;
            }
            Kind::Eye => {
                self.eye += 1;
                self.eye_state.last_action = Some(now);
                self.eye_state.waiting = false;
            }
        }
        self.save_history_snapshot();
    }

    pub fn set_interval(&mut self, kind: Kind, mins: u32) {
        let now = Instant::now();
        match kind {
            Kind::Water => {
                self.cfg.water_min = mins;
                self.water_state.last_action = Some(now);
                self.water_state.waiting = false;
            }
            Kind::Stand => {
                self.cfg.stand_min = mins;
                self.stand_state.last_action = Some(now);
                self.stand_state.waiting = false;
            }
            Kind::Eye => {
                self.cfg.eye_min = mins;
                self.eye_state.last_action = Some(now);
                self.eye_state.waiting = false;
            }
        }
        storage::save_config(&self.cfg);
    }

    pub fn toggle_pause(&mut self) {
        self.paused = !self.paused;
        if !self.paused {
            let now = Instant::now();
            self.water_state = Reminder { last_action: Some(now), waiting: false };
            self.stand_state = Reminder { last_action: Some(now), waiting: false };
            self.eye_state = Reminder { last_action: Some(now), waiting: false };
        }
    }

    pub fn check_rollover(&mut self) {
        let today = Local::now().format("%Y-%m-%d").to_string();
        if today != self.today {
            self.save_history_snapshot();
            self.water = 0;
            self.stand = 0;
            self.eye = 0;
            self.work_start = Instant::now();
            self.today = today;
            self.lunch_done = false;
            self.off_work_done = false;
        }
    }

    pub fn set_lunch(&mut self, t: String) {
        self.cfg.lunch_time = t;
        self.lunch_done = false;
        storage::save_config(&self.cfg);
    }

    pub fn set_off_work(&mut self, t: String) {
        self.cfg.off_work = t;
        self.off_work_done = false;
        storage::save_config(&self.cfg);
    }
}

#[derive(Clone, Copy, Debug)]
pub enum Kind {
    Water,
    Stand,
    Eye,
}

pub type SharedState = Arc<Mutex<AppState>>;

pub fn parse_hhmm_minutes(s: &str) -> u32 {
    let parts: Vec<&str> = s.split(':').collect();
    if parts.len() != 2 {
        return 0;
    }
    let h: u32 = parts[0].parse().unwrap_or(0);
    let m: u32 = parts[1].parse().unwrap_or(0);
    h * 60 + m
}

pub fn today_date() -> NaiveDate {
    Local::now().date_naive()
}
