# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

## 项目概述

跨平台（macOS / Windows）系统托盘健康提醒小程序，单文件 Go 程序 (`main.go`)。通过系统托盘菜单提醒用户喝水、站立、护眼、午饭、下班，并按日记录统计数据。

## 构建与运行

```bash
./build.sh              # 根据当前平台构建（macOS 生成 dist/MyHealth.app，Windows 生成 dist/MyHealth.exe）
go run .                # 本地调试运行
go build -o MyHealth .  # 仅编译二进制
```

macOS 的 `Info.plist` 设置了 `LSUIElement=true`，应用无 Dock 图标，仅驻留菜单栏。Windows 使用 `-H windowsgui` 链接标志避免控制台窗口。

无单元测试。无 lint 配置；如需检查使用 `go vet ./...`。

## 架构要点

**单进程 + 全局状态 + 一把大锁**：所有共享状态（`cfg`、计数器、`ReminderState`、`holidays`、菜单项引用）由单个 `sync.Mutex mu` 保护。所有修改路径在持锁期间完成持久化（`saveConfig` / `saveHistory` / `saveHolidays`）。新增任何共享状态时务必同样接入此锁。

**三个后台 goroutine**：
1. `tickLoop` — 每秒循环：检测日期翻转（归档并重置当日计数）、按间隔触发提醒、检查午饭/下班定时、刷新菜单标题。
2. 菜单事件 goroutine — 用 `select` 监听主菜单项的 `ClickedCh`。
3. `intervalClickLoop` — 每 50ms 轮询子菜单项（喝水/站立/护眼间隔），因为 systray 的子菜单没有一体化的 select 事件流。

**阻塞式对话框代替原生通知**：
- 喝水、站立提醒通过 `osascript display dialog`（macOS）或 PowerShell `MessageBox`（Windows）同步弹出，用户点击按钮后才 `confirmAction`。状态机用 `ReminderState.waiting` 防止弹窗重入。
- 护眼提醒是非阻塞的 `showTimedDialog`（带 `giving up after` 自动关闭），立即计数，不等待用户。
- 午饭/下班同样使用 `showTimedDialog` 自动关闭。
- 新增提醒时注意：阻塞型必须走 `state.waiting=true → goroutine 弹窗 → confirmAction` 流程；非阻塞型直接 confirm。

**数据目录**：
- macOS: `~/Library/Application Support/MyHealth/`
- Windows: `%APPDATA%\MyHealth\`
- 三个 JSON 文件：`config.json`（间隔与午饭/下班时间）、`history.json`（按日 `DailyRecord` 列表）、`holidays.json`（节假日/调休）。

**节假日**：启动时 `syncHolidays` 异步请求 `https://timor.tech/api/holiday/year/{YEAR}`，替换当年的假日/调休日，保留其他年份本地数据。`isWorkday` 优先级：显式 workdays > 显式 holidays > `SkipWeekends` 判断。

**图标自包含生成**：`dropIcon` 手绘水滴图标为 RGBA，再用手写的 `rgbaToPNG`（含 `zlibStore` uncompressed deflate + adler32 + crc32 表）生成 PNG 字节。Windows 需要 ICO，用 `pngToICO` 包一层 ICONDIR（直接内嵌 PNG）。没有图像库依赖；修改图标时整条管线都在此文件内。

## 发布流程

使用 `/release vX.Y.Z` skill（`.claude/skills/release/SKILL.md`）。核心约束：

- **CHANGELOG 必须先于 tag 提交**。tag 指向的 commit 要已经包含该版本的 CHANGELOG 条目，否则 CI 构建出的 release notes 会缺内容。
- 顺序：更新 `CHANGELOG.md` → `git commit` → `git tag vX.Y.Z` → `git push origin main` → `git push origin vX.Y.Z`。
- CI 基于 tag 自动构建 macOS `.app` 与 Windows `.exe` 并发布 Release，无需手动上传产物。
- 如果已误先推 tag，默认只把 CHANGELOG 推到 main，下次 release 补；除非用户明确要求，不要 force-push tag（可能触发 CI 重跑或产物冲突）。

## 技术栈偏好

- **跨平台托盘应用优先 Go + `fyne.io/systray`**。此前 Tauri 方案在托盘图标/冻结上反复失败，已整体重写为当前 Go 版本。新建同类功能时不要重新评估 Tauri，除非托盘不是核心需求。
- 不引入图像处理库；图标用手写 PNG/ICO 管线（见 `dropIcon` / `rgbaToPNG` / `pngToICO`）。
- 不引入 PowerShell 以外的 Windows GUI 工具；对话框统一走 WPF + XAML（见 `winShowDialog` / `winShowInput`），参数通过环境变量传入避免转义。

## 修改时的注意事项

- 所有状态读写必须持 `mu`。通过 goroutine 触发的 UI 弹窗应在调用前释放锁、回写时再重新获取（参见 `showReminder`）。
- systray 菜单项 `SetTitle` 可在持锁期间调用（见 `updateMenu`）。
- `exec.Command` 里的 `osascript` / `powershell` 参数直接拼接字符串，用户输入（午饭/下班时间）已做 `HH:MM` 正则式校验，其他新增文案如引入用户输入需注意转义。