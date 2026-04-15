# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

## 项目概述

跨平台（macOS / Windows）系统托盘健康提醒小工具。Tauri v2 + Rust 后端 + 静态 HTML 前端（仅用于弹窗）。通过托盘菜单提醒用户喝水、站立、护眼、午饭、下班，并按日记录统计。

## 构建与运行

```bash
pnpm install                       # 首次安装前端依赖
cd src-tauri && cargo run          # 开发：直接跑后端，加载 dist/ 静态文件
pnpm tauri dev                     # 走 Tauri CLI（包含 vite dev server，目前无窗口实际不需要）
pnpm tauri build                   # 发布构建：macOS .app/.dmg + Windows .exe/.nsis
```

数据目录：
- macOS: `~/Library/Application Support/MyHealth/`
- Windows: `%APPDATA%\MyHealth\`
- 文件：`config.json`（间隔 + 午饭/下班时间）、`history.json`（按日 `DailyRecord` 列表）、`holidays.json`（节假日/调休）

## 架构要点

**Rust 模块拆分**（`src-tauri/src/`）：
- `lib.rs` — 入口，组装托盘 + 菜单事件分发 + 滴答循环 + 节假日同步线程
- `state.rs` — `AppState` 全局状态（计数器 / 提醒状态 / 配置 / 节假日），由 `parking_lot::Mutex` 保护，通过 `SharedState = Arc<Mutex<AppState>>` 在线程间共享
- `menu.rs` — 构造托盘菜单（含间隔子菜单、午饭/下班预设时间子菜单），暴露 `MenuRefs` 供 tick loop 更新标题
- `reminder.rs` — 弹窗逻辑：通过 `WebviewWindowBuilder` 即时创建无边框透明窗口加载 `dist/dialog.html`，参数走 query string
- `storage.rs` — JSON 持久化（config / history / holidays）
- `holidays.rs` — 启动时同步 `https://timor.tech/api/holiday/year/{YEAR}`，替换当年的 holidays/workdays，保留其他年份
- `icon.rs` — 手绘 22×22 蓝色水滴 RGBA，直接交给 Tauri `Image::new_owned`（不再需要 PNG/ICO 编码，Tauri 用 image crate 处理）

**全局锁**：`AppState` 一把 `parking_lot::Mutex`。所有状态读写必须持锁。**触发弹窗前必须释放锁**（弹窗内部会重新加锁回写计数）—— 见 `tick_loop` 把 `triggers` 收集后再 drop guard。

**两个后台线程**：
1. `tick_loop`（`lib.rs`）—— 每秒：检测日期翻转、按间隔触发提醒、定点检查午饭/下班、刷新菜单标题
2. `holidays::sync` —— 一次性，启动后请求节假日 API

**菜单事件**：Tauri v2 的 `TrayIconBuilder::on_menu_event` 单点回调，按 `event.id` 字符串前缀路由（`water_int_30` / `lunch_12:00` 等）。子菜单 ☑ 切换在 Rust 侧通过 `CheckMenuItem::set_checked` 显式同步。

**弹窗策略**：
- 阻塞型（喝水 / 站立）：`reminder::show_blocking` 在工作线程创建窗口，`mpsc::channel` 阻塞等 `WindowEvent::Destroyed`，关闭后才 `confirm()` 计数。状态机用 `Reminder.waiting=true` 防重入
- 非阻塞（护眼 / 午饭 / 下班）：`show_timed` 创建窗口立即返回，前端 `dialog.html` 自身用 `setInterval` 倒计时关闭，护眼立即 `confirm()`
- 弹窗 HTML 在 `dist/dialog.html` —— 单文件内联 CSS/JS，Win11/macOS 暗色模式自适应（`prefers-color-scheme`）。**不走 Vite 构建**，直接被 Tauri 当静态资源加载。修改样式只改这一个文件

**节假日**：`isWorkday` 优先级：显式 workdays > 显式 holidays > `SkipWeekends`（默认 true，跳过周末）。

## 发布流程

使用 `/release vX.Y.Z` skill（`.claude/skills/release/SKILL.md`）。核心约束：

- **CHANGELOG 必须先于 tag 提交**。tag 指向的 commit 要已经包含该版本的 CHANGELOG 条目，否则 CI 构建出的 release notes 会缺内容
- 顺序：更新 `CHANGELOG.md` → `git commit` → `git tag vX.Y.Z` → `git push origin main` → `git push origin vX.Y.Z`
- 版本号同步更新 `src-tauri/tauri.conf.json` 和 `src-tauri/Cargo.toml` 的 `version` 字段，以及 `package.json`
- CI 基于 tag 自动用 `tauri build` 打包 macOS / Windows 产物并发布 Release
- 如果已误先推 tag，默认只把 CHANGELOG 推到 main，下次 release 补；不要 force-push tag

## 技术栈偏好

- **托盘应用栈**：Tauri v2（v2 stable 起 tray 已可用，底层 `muda` + `tao`）。前一版 Go + `fyne.io/systray` 已淘汰，归档历史在 git 里可查
- **弹窗 UI**：HTML/CSS in `dist/dialog.html`，不引入前端框架。修改样式直接改 CSS 变量（`--accent` / `--panel-bg` 等）
- **不引入 Node 前端框架**：本应用无主窗口，前端只是几张静态 HTML，没必要上 React/Vue
- **Rust 依赖最少化**：当前依赖 `tauri / serde / serde_json / chrono / dirs / reqwest(blocking,rustls) / parking_lot / urlencoding`。新加依赖前先想能不能用标准库

## macOS 特殊处理

- `set_activation_policy(ActivationPolicy::Accessory)` 在 `setup` 中调用，去掉 Dock 图标
- 托盘图标 `icon_as_template(true)` 让 macOS 自动适配深色/浅色模式（图标用单色蓝）
- `tauri.conf.json` 启用 `macOSPrivateApi: true`，弹窗窗口才能透明（`transparent: true`）

## 修改时的注意事项

- 所有 `AppState` 读写持 `parking_lot::Mutex` 锁。**触发弹窗前 drop 锁守卫**，避免弹窗回调死锁
- 新增菜单项：在 `menu::build` 加构造 + 加入 `Menu::with_items` 列表 + 在 `MenuRefs` 暴露引用（如需运行时改文本/勾选）+ 在 `lib.rs::handle_menu_event` 加分支
- 新增提醒种类：在 `state::Kind` 加变体 + `AppState::confirm` / `set_interval` 加分支 + `reminder::dispatch_reminder` 加分支 + tick loop 加触发
- 修改弹窗样式只改 `dist/dialog.html`，不需要重新编译 Rust
