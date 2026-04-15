use serde::{Deserialize, Serialize};
use std::fs;
use std::path::{Path, PathBuf};

#[derive(Serialize, Deserialize, Clone, Debug)]
pub struct Config {
    #[serde(rename = "water_interval_min")]
    pub water_min: u32,
    #[serde(rename = "stand_interval_min")]
    pub stand_min: u32,
    #[serde(rename = "eye_rest_interval_min")]
    pub eye_min: u32,
    #[serde(rename = "lunch_time")]
    pub lunch_time: String,
    #[serde(rename = "off_work_time")]
    pub off_work: String,
}

impl Default for Config {
    fn default() -> Self {
        Self {
            water_min: 30,
            stand_min: 45,
            eye_min: 20,
            lunch_time: "12:00".into(),
            off_work: "18:00".into(),
        }
    }
}

#[derive(Serialize, Deserialize, Clone, Debug)]
pub struct DailyRecord {
    pub date: String,
    #[serde(rename = "water_count")]
    pub water: u32,
    #[serde(rename = "stand_count")]
    pub stand: u32,
    #[serde(rename = "eye_rest_count")]
    pub eye: u32,
    #[serde(rename = "work_minutes")]
    pub work_min: u32,
}

#[derive(Serialize, Deserialize, Clone, Debug, Default)]
pub struct HolidayConfig {
    #[serde(rename = "skipWeekends", default = "default_true")]
    pub skip_weekends: bool,
    #[serde(default)]
    pub holidays: Vec<String>,
    #[serde(default)]
    pub workdays: Vec<String>,
}

fn default_true() -> bool {
    true
}

pub fn data_dir() -> PathBuf {
    let base = if cfg!(target_os = "macos") {
        dirs::home_dir()
            .unwrap_or_else(|| PathBuf::from("."))
            .join("Library/Application Support")
    } else {
        dirs::config_dir().unwrap_or_else(|| PathBuf::from("."))
    };
    let dir = base.join("MyHealth");
    let _ = fs::create_dir_all(&dir);
    dir
}

pub fn load_json<T: for<'de> Deserialize<'de> + Default>(path: &Path) -> T {
    fs::read_to_string(path)
        .ok()
        .and_then(|s| serde_json::from_str::<T>(&s).ok())
        .unwrap_or_default()
}

pub fn save_json<T: Serialize>(path: &Path, v: &T) {
    if let Ok(s) = serde_json::to_string_pretty(v) {
        let _ = fs::write(path, s);
    }
}

pub fn load_config() -> Config {
    let path = data_dir().join("config.json");
    if path.exists() {
        fs::read_to_string(&path)
            .ok()
            .and_then(|s| serde_json::from_str::<Config>(&s).ok())
            .unwrap_or_default()
    } else {
        Config::default()
    }
}

pub fn save_config(cfg: &Config) {
    save_json(&data_dir().join("config.json"), cfg);
}

pub fn load_holidays() -> HolidayConfig {
    let path = data_dir().join("holidays.json");
    if path.exists() {
        fs::read_to_string(&path)
            .ok()
            .and_then(|s| serde_json::from_str::<HolidayConfig>(&s).ok())
            .unwrap_or_else(|| HolidayConfig {
                skip_weekends: true,
                ..Default::default()
            })
    } else {
        HolidayConfig {
            skip_weekends: true,
            ..Default::default()
        }
    }
}

pub fn save_holidays(h: &HolidayConfig) {
    save_json(&data_dir().join("holidays.json"), h);
}

pub fn load_history() -> Vec<DailyRecord> {
    load_json(&data_dir().join("history.json"))
}

pub fn upsert_today(rec: DailyRecord) {
    let path = data_dir().join("history.json");
    let mut records: Vec<DailyRecord> = load_json(&path);
    if let Some(r) = records.iter_mut().find(|r| r.date == rec.date) {
        *r = rec;
    } else {
        records.push(rec);
    }
    save_json(&path, &records);
}
