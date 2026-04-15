# Changelog

## v0.1.0

### 重大变更
- **整体重写为 Tauri v2 + Rust**。前一版 Go + `fyne.io/systray` 已归档（git 历史可查）
- Windows 弹窗不再依赖 PowerShell —— WebView 直接渲染 HTML，杀软不再误报

### 新功能
- 弹窗使用 HTML/CSS 渲染：圆角卡片 + 阴影 + 透明背景 + Win11/macOS 风格 Accent 按钮，自适应深色/浅色模式
- 设置午饭/下班时间改回自由输入弹窗，HH:MM 实时校验
- 提醒倒计时实时刷新（菜单标题秒级更新）

### 内部
- Rust 模块拆分（`state` / `menu` / `reminder` / `storage` / `holidays` / `icon`），全局状态用 `parking_lot::Mutex` 保护
- 托盘图标改为 44×44 抗锯齿手绘水滴（`icon.rs`），保留蓝色（关闭 macOS template 模式）
- CI 重写为 `tauri-action`，自动构建 macOS aarch64/x86_64 + Windows，发布 `.dmg` / `.app` / `.exe` / `.msi`

## v0.0.3

### 改进
- Windows 弹窗升级为 Windows 11 风格（WPF + XAML）：圆角 + 阴影 + Segoe UI + 蓝色 Accent 按钮，悬停/按下态
- 设置午饭/下班时间的输入框改为 WPF 现代样式（圆角 TextBox、默认值自动全选聚焦、确定/取消双按钮）
- 午饭/下班提醒通过 `DispatcherTimer` 自动关闭，不再依赖老旧 `MessageBox`

### 内部
- PowerShell 参数走环境变量传入，避免引号转义问题

## v0.0.2

### 新功能
- 眼睛休息提醒改为非阻塞通知，6 秒后自动关闭，无需手动确认即自动计入今日统计

### 修复
- Windows 托盘图标不显示的问题（Windows 下将 PNG 包装为 ICO 格式）

## v0.0.1

首个跨平台版本。

### 核心功能
- 系统托盘水滴图标 + 菜单
- 喝水、站立、护眼三项定时提醒（可自定义间隔）
- 午饭、下班定点提醒（自定义时间）
- 下班提醒自动跳过周末和法定节假日
- 节假日数据每日自动从 `timor.tech` API 同步
- 每日统计（喝水/站立/护眼次数、工作时长）
- 历史记录持久化（`history.json`）
- 配置持久化（`config.json`）
- 暂停/继续、跨天自动重置

### 平台支持
- macOS（.app bundle，LSUIElement 隐藏 Dock 图标）
- Windows 11（.exe，`-H windowsgui` 隐藏控制台）
