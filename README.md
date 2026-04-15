# MyHealth

跨平台（macOS / Windows）系统托盘健康提醒小工具。

驻留在菜单栏 / 任务栏，定时提醒你 **喝水 / 站立 / 护眼**，定点提醒 **午饭 / 下班**，按日记录统计。

## 功能

- 🟦 菜单栏蓝色水滴图标，秒级倒计时刷新
- 💧 喝水、🧍 站立、👀 护眼三项可自定义间隔（15 / 20 / 30 / 45 / 60 等预设）
- 🍚 午饭、🏠 下班定点提醒，时间自由输入（HH:MM）
- 🎨 弹窗用 HTML/CSS 渲染，Win11 Fluent / macOS Vibrancy 风格，自适应深色/浅色模式
- 📅 自动同步法定节假日 / 调休（数据源 [timor.tech](https://timor.tech)），下班提醒自动跳过周末和节假日
- 📊 按日记录喝水/站立/护眼次数 + 工作时长，菜单一键打开 `history.json`
- ⏸ 一键暂停/继续，跨天自动归档重置
- 🛡 Windows 不依赖 PowerShell，杀软不再误报

## 下载

去 [Releases](https://github.com/LYF1999/my_health/releases) 选最新版：

- **macOS**：下载 `.dmg`（Apple Silicon 选 `aarch64`，Intel 选 `x64`）。首次打开被 Gatekeeper 拦，右键 → 打开 即可
- **Windows**：下载 `.msi` 或 `.exe` 安装包。SmartScreen 警告 → 更多信息 → 仍要运行

## 数据目录

配置和历史本地保存：

- macOS：`~/Library/Application Support/MyHealth/`
- Windows：`%APPDATA%\MyHealth\`

包含三个 JSON 文件：

- `config.json` —— 提醒间隔 + 午饭/下班时间
- `history.json` —— 按日统计记录
- `holidays.json` —— 当年节假日 / 调休（首次启动从 API 同步）

## 从源码构建

需要 **Rust**（stable 通道）+ **Node 20+** + **pnpm 10**。

```bash
pnpm install
cd src-tauri && cargo run             # 开发：直接跑（debug 模式）
pnpm tauri build                      # 发布：生成 .app/.dmg/.exe/.msi
```

## 架构

Tauri v2 + Rust 后端 + 静态 HTML 弹窗（无前端框架）。详见 [CLAUDE.md](./CLAUDE.md)。

## 历史

v0.0.x 用 Go + `fyne.io/systray` 实现。v0.1.0 起整体重写为 Tauri v2，旧版可在 git 历史里查 `v0.0.3` tag。
