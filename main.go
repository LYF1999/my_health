package main

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"sync"
	"time"

	"fyne.io/systray"
)

// ─── 数据结构 ───

type Config struct {
	WaterMin   int    `json:"water_interval_min"`
	StandMin   int    `json:"stand_interval_min"`
	EyeRestMin int    `json:"eye_rest_interval_min"`
	LunchTime  string `json:"lunch_time"`
	OffWork    string `json:"off_work_time"`
}

type DailyRecord struct {
	Date        string `json:"date"`
	WaterCount  int    `json:"water_count"`
	StandCount  int    `json:"stand_count"`
	EyeRestCnt  int    `json:"eye_rest_count"`
	WorkMinutes int    `json:"work_minutes"`
}

type HolidayConfig struct {
	SkipWeekends bool     `json:"skipWeekends"`
	Holidays     []string `json:"holidays"`
	Workdays     []string `json:"workdays"`
}

type ReminderState struct {
	lastAction time.Time
	waiting    bool
}

// ─── 全局状态 ───

var (
	mu          sync.Mutex
	cfg         Config
	paused      bool
	waterCount  int
	standCount  int
	eyeRestCnt  int
	workStart   time.Time
	todayStr    string
	lunchDone   bool
	offWorkDone bool

	waterState   = ReminderState{}
	standState   = ReminderState{}
	eyeRestState = ReminderState{}

	holidays HolidayConfig
	dataDir  string

	// 菜单项引用
	mWaterTimer   *systray.MenuItem
	mStandTimer   *systray.MenuItem
	mEyeRestTimer *systray.MenuItem
	mWorkTime     *systray.MenuItem
	mStats        *systray.MenuItem
	mPause        *systray.MenuItem
	mLunch        *systray.MenuItem
	mOffWork      *systray.MenuItem

	// 间隔子菜单项（用于 ☑️ 标记）
	waterIntItems   []*systray.MenuItem
	waterIntValues  []int
	standIntItems   []*systray.MenuItem
	standIntValues  []int
	eyeIntItems     []*systray.MenuItem
	eyeIntValues    []int
)

func main() {
	systray.Run(onReady, onExit)
}

func onReady() {
	dataDir = getDataDir()
	loadConfig()
	loadHolidays()

	now := time.Now()
	workStart = now
	todayStr = now.Format("2006-01-02")
	waterState.lastAction = now
	standState.lastAction = now
	eyeRestState.lastAction = now

	systray.SetIcon(dropIcon())
	systray.SetTooltip("健康提醒小助手")

	// ─── 构建菜单 ───
	systray.AddMenuItem("健康提醒小助手", "").Disable()
	systray.AddSeparator()

	mWaterTimer = systray.AddMenuItem("下次喝水: --:--", "")
	mWaterTimer.Disable()
	mStandTimer = systray.AddMenuItem("下次站立: --:--", "")
	mStandTimer.Disable()
	mEyeRestTimer = systray.AddMenuItem("下次护眼: --:--", "")
	mEyeRestTimer.Disable()
	systray.AddSeparator()

	mWorkTime = systray.AddMenuItem("今日工作: 0h0m", "")
	mWorkTime.Disable()
	mStats = systray.AddMenuItem("今日统计: 0/0/0", "")
	mStats.Disable()
	systray.AddSeparator()

	mRecordWater := systray.AddMenuItem("记录喝水", "")
	mRecordStand := systray.AddMenuItem("记录站立", "")
	systray.AddSeparator()

	// 间隔子菜单（带 ☑️）
	mWaterInt := systray.AddMenuItem("喝水间隔", "")
	waterIntValues = []int{15, 20, 30, 45, 60}
	for _, m := range waterIntValues {
		item := mWaterInt.AddSubMenuItemCheckbox(fmt.Sprintf("%d 分钟", m), "", m == cfg.WaterMin)
		waterIntItems = append(waterIntItems, item)
	}

	mStandInt := systray.AddMenuItem("站立间隔", "")
	standIntValues = []int{20, 30, 45, 60, 90}
	for _, m := range standIntValues {
		item := mStandInt.AddSubMenuItemCheckbox(fmt.Sprintf("%d 分钟", m), "", m == cfg.StandMin)
		standIntItems = append(standIntItems, item)
	}

	mEyeInt := systray.AddMenuItem("护眼间隔", "")
	eyeIntValues = []int{10, 15, 20, 25, 30}
	for _, m := range eyeIntValues {
		item := mEyeInt.AddSubMenuItemCheckbox(fmt.Sprintf("%d 分钟", m), "", m == cfg.EyeRestMin)
		eyeIntItems = append(eyeIntItems, item)
	}

	mLunch = systray.AddMenuItem(fmt.Sprintf("午饭提醒:  %s", cfg.LunchTime), "")
	mOffWork = systray.AddMenuItem(fmt.Sprintf("下班提醒:  %s", cfg.OffWork), "")
	systray.AddSeparator()

	mPause = systray.AddMenuItem("暂停提醒", "")
	mHistory := systray.AddMenuItem("查看历史记录...", "")
	systray.AddSeparator()
	mQuit := systray.AddMenuItem("退出", "")

	// 后台定时器
	go tickLoop()
	go syncHolidays()

	// ─── 菜单事件 ───
	go func() {
		for {
			select {
			case <-mRecordWater.ClickedCh:
				record("water")
			case <-mRecordStand.ClickedCh:
				record("stand")
			case <-mPause.ClickedCh:
				togglePause()
			case <-mLunch.ClickedCh:
				promptSetTime("lunch")
			case <-mOffWork.ClickedCh:
				promptSetTime("offwork")
			case <-mHistory.ClickedCh:
				openFile(filepath.Join(dataDir, "history.json"))
			case <-mQuit.ClickedCh:
				mu.Lock()
				saveHistory()
				saveConfig()
				mu.Unlock()
				systray.Quit()
			}
		}
	}()

	// 间隔选择事件
	go intervalClickLoop(waterIntItems, waterIntValues, "water")
	go intervalClickLoop(standIntItems, standIntValues, "stand")
	go intervalClickLoop(eyeIntItems, eyeIntValues, "eyeRest")
}

func intervalClickLoop(items []*systray.MenuItem, values []int, rtype string) {
	cases := make([]reflect_case, len(items))
	for i, item := range items {
		cases[i] = reflect_case{ch: item.ClickedCh, val: values[i]}
	}
	for {
		// 手动轮询每个 channel
		for i, item := range items {
			select {
			case <-item.ClickedCh:
				setInterval(rtype, values[i])
				// 更新 ☑️
				for j, it := range items {
					if j == i {
						it.Check()
					} else {
						it.Uncheck()
					}
				}
			default:
			}
		}
		time.Sleep(50 * time.Millisecond)
	}
}

type reflect_case struct {
	ch  chan struct{}
	val int
}

func onExit() {}

// ─── 定时循环 ───

func tickLoop() {
	for {
		time.Sleep(1 * time.Second)
		mu.Lock()

		today := time.Now().Format("2006-01-02")
		if today != todayStr {
			saveHistory()
			waterCount, standCount, eyeRestCnt = 0, 0, 0
			workStart = time.Now()
			todayStr = today
			lunchDone, offWorkDone = false, false
		}

		if !paused {
			checkReminder("water", &waterState, time.Duration(cfg.WaterMin)*time.Minute)
			checkReminder("stand", &standState, time.Duration(cfg.StandMin)*time.Minute)
			checkReminder("eyeRest", &eyeRestState, time.Duration(cfg.EyeRestMin)*time.Minute)
			checkScheduled()
		}

		updateMenu()
		mu.Unlock()
	}
}

func checkReminder(rtype string, state *ReminderState, interval time.Duration) {
	if state.waiting {
		return
	}
	if time.Since(state.lastAction) >= interval {
		state.waiting = true
		go showReminder(rtype)
	}
}

func checkScheduled() {
	now := time.Now()
	mins := now.Hour()*60 + now.Minute()

	lunchMins := parseTimeMinutes(cfg.LunchTime)
	if !lunchDone && mins >= lunchMins && mins < lunchMins+30 {
		lunchDone = true
		go showTimedDialog("🍚 该吃午饭了", "别忘了吃午饭！按时吃饭，身体才能保持好状态。", 8)
	}

	offMins := parseTimeMinutes(cfg.OffWork)
	if !offWorkDone && isWorkday() && mins >= offMins && mins < offMins+30 {
		offWorkDone = true
		wm := int(time.Since(workStart).Minutes())
		msg := fmt.Sprintf("今天已工作 %d 小时 %d 分钟，辛苦了！该休息了。", wm/60, wm%60)
		go showTimedDialog("🏠 该下班了", msg, 10)
	}
}

func updateMenu() {
	ft := func(state *ReminderState, interval int) string {
		if paused {
			return "已暂停"
		}
		if state.waiting {
			return "等待确认..."
		}
		left := time.Duration(interval)*time.Minute - time.Since(state.lastAction)
		if left < 0 {
			left = 0
		}
		s := int(left.Seconds())
		return fmt.Sprintf("%02d:%02d", s/60, s%60)
	}

	mWaterTimer.SetTitle(fmt.Sprintf("下次喝水:  %s", ft(&waterState, cfg.WaterMin)))
	mStandTimer.SetTitle(fmt.Sprintf("下次站立:  %s", ft(&standState, cfg.StandMin)))
	mEyeRestTimer.SetTitle(fmt.Sprintf("下次护眼:  %s", ft(&eyeRestState, cfg.EyeRestMin)))

	wm := int(time.Since(workStart).Minutes())
	day := "工作日"
	if !isWorkday() {
		day = "休息日"
	}
	mWorkTime.SetTitle(fmt.Sprintf("今日工作:  %dh%dm · %s", wm/60, wm%60, day))
	mStats.SetTitle(fmt.Sprintf("今日统计:  喝水%d · 站立%d · 护眼%d", waterCount, standCount, eyeRestCnt))

	mLunch.SetTitle(fmt.Sprintf("午饭提醒:  %s", cfg.LunchTime))
	mOffWork.SetTitle(fmt.Sprintf("下班提醒:  %s", cfg.OffWork))

	if paused {
		mPause.SetTitle("继续提醒")
	} else {
		mPause.SetTitle("暂停提醒")
	}
}

// ─── 操作 ───

func record(rtype string) {
	mu.Lock()
	defer mu.Unlock()
	confirmAction(rtype)
}

func confirmAction(rtype string) {
	switch rtype {
	case "water":
		waterCount++
		waterState.lastAction = time.Now()
		waterState.waiting = false
	case "stand":
		standCount++
		standState.lastAction = time.Now()
		standState.waiting = false
	case "eyeRest":
		eyeRestCnt++
		eyeRestState.lastAction = time.Now()
		eyeRestState.waiting = false
	}
	saveHistory()
}

func setInterval(rtype string, mins int) {
	mu.Lock()
	defer mu.Unlock()
	switch rtype {
	case "water":
		cfg.WaterMin = mins
		waterState.lastAction = time.Now()
		waterState.waiting = false
	case "stand":
		cfg.StandMin = mins
		standState.lastAction = time.Now()
		standState.waiting = false
	case "eyeRest":
		cfg.EyeRestMin = mins
		eyeRestState.lastAction = time.Now()
		eyeRestState.waiting = false
	}
	saveConfig()
}

func togglePause() {
	mu.Lock()
	defer mu.Unlock()
	paused = !paused
	if !paused {
		now := time.Now()
		waterState.lastAction = now
		standState.lastAction = now
		eyeRestState.lastAction = now
		waterState.waiting = false
		standState.waiting = false
		eyeRestState.waiting = false
	}
}

// ─── 弹窗 ───

func showReminder(rtype string) {
	title := ""
	body := ""
	btn := ""
	switch rtype {
	case "water":
		title = "健康提醒"
		body = "💧 该喝水了！起来倒杯水吧。"
		btn = "喝水了"
	case "stand":
		title = "健康提醒"
		body = "🧍 该站起来了！走走伸展一下。"
		btn = "站立了"
	case "eyeRest":
		title = "健康提醒"
		body = "👀 该休息眼睛了！看看远处 20 秒。"
		btn = "休息了"
	}

	if runtime.GOOS == "darwin" {
		exec.Command("osascript", "-e",
			fmt.Sprintf(`display dialog "%s" buttons {"%s"} default button 1 with title "%s"`, body, btn, title),
		).Run()
	} else {
		exec.Command("powershell", "-Command",
			fmt.Sprintf(`Add-Type -AssemblyName System.Windows.Forms; [System.Windows.Forms.MessageBox]::Show('%s','%s','OK','Information')`, body, title),
		).Run()
	}

	mu.Lock()
	confirmAction(rtype)
	mu.Unlock()
}

// 定时自动关闭的提醒（午饭/下班）
func showTimedDialog(title, body string, seconds int) {
	if runtime.GOOS == "darwin" {
		exec.Command("osascript", "-e",
			fmt.Sprintf(`display dialog "%s" buttons {"知道了"} default button 1 giving up after %d with title "%s"`, body, seconds, title),
		).Run()
	} else {
		// Windows: 用 PowerShell 弹出自动关闭的消息框
		exec.Command("powershell", "-Command",
			fmt.Sprintf(`Add-Type -AssemblyName System.Windows.Forms; $f = New-Object Windows.Forms.Form -Property @{TopMost=$true}; [System.Windows.Forms.MessageBox]::Show($f, '%s', '%s', 'OK', 'Information')`, body, title),
		).Run()
	}
}

// 弹窗让用户输入时间
func promptSetTime(which string) {
	currentVal := ""
	prompt := ""
	mu.Lock()
	if which == "lunch" {
		currentVal = cfg.LunchTime
		prompt = "请输入午饭提醒时间（格式 HH:MM）："
	} else {
		currentVal = cfg.OffWork
		prompt = "请输入下班提醒时间（格式 HH:MM）："
	}
	mu.Unlock()

	var result string
	if runtime.GOOS == "darwin" {
		out, err := exec.Command("osascript", "-e",
			fmt.Sprintf(`set r to display dialog "%s" default answer "%s" with title "设置时间" buttons {"取消","确定"} default button 2
if button returned of r is "确定" then return text returned of r`, prompt, currentVal),
		).Output()
		if err != nil {
			return
		}
		result = string(out)
	} else {
		out, err := exec.Command("powershell", "-Command",
			fmt.Sprintf(`Add-Type -AssemblyName Microsoft.VisualBasic; [Microsoft.VisualBasic.Interaction]::InputBox('%s', '设置时间', '%s')`, prompt, currentVal),
		).Output()
		if err != nil {
			return
		}
		result = string(out)
	}

	// 去掉换行
	for len(result) > 0 && (result[len(result)-1] == '\n' || result[len(result)-1] == '\r') {
		result = result[:len(result)-1]
	}
	if result == "" {
		return
	}

	// 验证格式
	var h, m int
	if _, err := fmt.Sscanf(result, "%d:%d", &h, &m); err != nil || h < 0 || h > 23 || m < 0 || m > 59 {
		return
	}
	timeStr := fmt.Sprintf("%02d:%02d", h, m)

	mu.Lock()
	if which == "lunch" {
		cfg.LunchTime = timeStr
		lunchDone = false
	} else {
		cfg.OffWork = timeStr
		offWorkDone = false
	}
	saveConfig()
	mu.Unlock()
}

// ─── 节假日 ───

func isWorkday() bool {
	today := time.Now().Format("2006-01-02")
	for _, d := range holidays.Workdays {
		if d == today {
			return true
		}
	}
	for _, d := range holidays.Holidays {
		if d == today {
			return false
		}
	}
	if holidays.SkipWeekends {
		wd := time.Now().Weekday()
		if wd == time.Saturday || wd == time.Sunday {
			return false
		}
	}
	return true
}

func syncHolidays() {
	year := time.Now().Year()
	url := fmt.Sprintf("https://timor.tech/api/holiday/year/%d", year)
	resp, err := http.Get(url)
	if err != nil {
		return
	}
	defer resp.Body.Close()
	data, _ := io.ReadAll(resp.Body)

	var result struct {
		Code    int `json:"code"`
		Holiday map[string]struct {
			Holiday bool   `json:"holiday"`
			Date    string `json:"date"`
		} `json:"holiday"`
	}
	if json.Unmarshal(data, &result) != nil || result.Code != 0 {
		return
	}

	mu.Lock()
	prefix := fmt.Sprintf("%d-", year)
	var newH, newW []string
	for _, d := range holidays.Holidays {
		if len(d) >= len(prefix) && d[:len(prefix)] != prefix {
			newH = append(newH, d)
		}
	}
	for _, d := range holidays.Workdays {
		if len(d) >= len(prefix) && d[:len(prefix)] != prefix {
			newW = append(newW, d)
		}
	}
	for _, e := range result.Holiday {
		if e.Holiday {
			newH = append(newH, e.Date)
		} else {
			newW = append(newW, e.Date)
		}
	}
	holidays.Holidays = newH
	holidays.Workdays = newW
	mu.Unlock()
	saveHolidays()
}

// ─── 持久化 ───

func getDataDir() string {
	var base string
	if runtime.GOOS == "darwin" {
		base = filepath.Join(os.Getenv("HOME"), "Library", "Application Support")
	} else {
		base = os.Getenv("APPDATA")
		if base == "" {
			base = "."
		}
	}
	dir := filepath.Join(base, "MyHealth")
	os.MkdirAll(dir, 0755)
	return dir
}

func loadConfig() {
	cfg = Config{WaterMin: 30, StandMin: 45, EyeRestMin: 20, LunchTime: "12:00", OffWork: "18:00"}
	data, err := os.ReadFile(filepath.Join(dataDir, "config.json"))
	if err == nil {
		json.Unmarshal(data, &cfg)
	}
}

func saveConfig() {
	data, _ := json.MarshalIndent(cfg, "", "  ")
	os.WriteFile(filepath.Join(dataDir, "config.json"), data, 0644)
}

func loadHolidays() {
	holidays = HolidayConfig{SkipWeekends: true}
	data, err := os.ReadFile(filepath.Join(dataDir, "holidays.json"))
	if err == nil {
		json.Unmarshal(data, &holidays)
	}
}

func saveHolidays() {
	mu.Lock()
	data, _ := json.MarshalIndent(holidays, "", "  ")
	mu.Unlock()
	os.WriteFile(filepath.Join(dataDir, "holidays.json"), data, 0644)
}

func saveHistory() {
	path := filepath.Join(dataDir, "history.json")
	var records []DailyRecord
	if data, err := os.ReadFile(path); err == nil {
		json.Unmarshal(data, &records)
	}

	today := time.Now().Format("2006-01-02")
	wm := int(time.Since(workStart).Minutes())
	rec := DailyRecord{
		Date: today, WaterCount: waterCount, StandCount: standCount,
		EyeRestCnt: eyeRestCnt, WorkMinutes: wm,
	}

	found := false
	for i, r := range records {
		if r.Date == today {
			records[i] = rec
			found = true
			break
		}
	}
	if !found {
		records = append(records, rec)
	}

	data, _ := json.MarshalIndent(records, "", "  ")
	os.WriteFile(path, data, 0644)
}

func parseTimeMinutes(s string) int {
	var h, m int
	fmt.Sscanf(s, "%d:%d", &h, &m)
	return h*60 + m
}

func openFile(path string) {
	if runtime.GOOS == "darwin" {
		exec.Command("open", path).Start()
	} else {
		exec.Command("cmd", "/c", "start", path).Start()
	}
}

// ─── 图标 ───

func dropIcon() []byte {
	const w, h = 22, 22
	rgba := make([]byte, w*h*4)
	cx := float64(w) / 2.0
	for y := range h {
		for x := range w {
			dx := float64(x) - cx
			fy := float64(y)
			cy := 13.0
			r := 7.0
			inCircle := dx*dx+(fy-cy)*(fy-cy) <= r*r
			inTri := fy >= 2 && fy <= cy-r+2 && func() bool {
				t := (fy - 2) / (cy - r + 2 - 2)
				return dx >= -r*t && dx <= r*t
			}()
			if inCircle || inTri {
				off := (y*w + x) * 4
				rgba[off] = 59
				rgba[off+1] = 130
				rgba[off+2] = 246
				rgba[off+3] = 255
			}
		}
	}
	return rgbaToPNG(w, h, rgba)
}

func rgbaToPNG(width, height int, rgba []byte) []byte {
	var b []byte
	b = append(b, 0x89, 'P', 'N', 'G', 0x0d, 0x0a, 0x1a, 0x0a)
	ihdr := []byte{
		byte(width >> 24), byte(width >> 16), byte(width >> 8), byte(width),
		byte(height >> 24), byte(height >> 16), byte(height >> 8), byte(height),
		8, 6, 0, 0, 0,
	}
	b = append(b, pngChunk("IHDR", ihdr)...)
	var raw []byte
	for y := range height {
		raw = append(raw, 0)
		raw = append(raw, rgba[y*width*4:(y+1)*width*4]...)
	}
	b = append(b, pngChunk("IDAT", zlibStore(raw))...)
	b = append(b, pngChunk("IEND", nil)...)
	return b
}

func pngChunk(ctype string, data []byte) []byte {
	length := len(data)
	chunk := make([]byte, 4+4+length+4)
	chunk[0] = byte(length >> 24)
	chunk[1] = byte(length >> 16)
	chunk[2] = byte(length >> 8)
	chunk[3] = byte(length)
	copy(chunk[4:8], ctype)
	copy(chunk[8:], data)
	crc := crc32Calc(chunk[4 : 8+length])
	chunk[8+length] = byte(crc >> 24)
	chunk[8+length+1] = byte(crc >> 16)
	chunk[8+length+2] = byte(crc >> 8)
	chunk[8+length+3] = byte(crc)
	return chunk
}

func crc32Calc(data []byte) uint32 {
	crc := uint32(0xFFFFFFFF)
	for _, b := range data {
		crc = crc32Table[(crc^uint32(b))&0xFF] ^ (crc >> 8)
	}
	return crc ^ 0xFFFFFFFF
}

var crc32Table [256]uint32

func init() {
	for i := range 256 {
		c := uint32(i)
		for range 8 {
			if c&1 != 0 {
				c = 0xEDB88320 ^ (c >> 1)
			} else {
				c >>= 1
			}
		}
		crc32Table[i] = c
	}
}

func zlibStore(data []byte) []byte {
	var out []byte
	out = append(out, 0x78, 0x01)
	remaining := data
	for len(remaining) > 0 {
		sz := len(remaining)
		last := byte(0)
		if sz <= 65535 {
			last = 1
		} else {
			sz = 65535
		}
		out = append(out, last, byte(sz), byte(sz>>8), byte(^sz&0xFF), byte((^sz>>8)&0xFF))
		out = append(out, remaining[:sz]...)
		remaining = remaining[sz:]
	}
	a, b := uint32(1), uint32(0)
	for _, d := range data {
		a = (a + uint32(d)) % 65521
		b = (b + a) % 65521
	}
	adler := (b << 16) | a
	out = append(out, byte(adler>>24), byte(adler>>16), byte(adler>>8), byte(adler))
	return out
}
