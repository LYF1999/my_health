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
	// 眼睛休息：非阻塞提示，自动关闭，直接计数
	if rtype == "eyeRest" {
		go showTimedDialog("👀 该休息眼睛了", "看看 6 米外的远处，持续 20 秒，放松一下眼睛。", 6)
		mu.Lock()
		confirmAction(rtype)
		mu.Unlock()
		return
	}

	title := "健康提醒"
	var body, btn string
	switch rtype {
	case "water":
		body = "💧 该喝水了！起来倒杯水吧。"
		btn = "喝水了"
	case "stand":
		body = "🧍 该站起来了！走走伸展一下。"
		btn = "站立了"
	}

	if runtime.GOOS == "darwin" {
		exec.Command("osascript", "-e",
			fmt.Sprintf(`display dialog "%s" buttons {"%s"} default button 1 with title "%s"`, body, btn, title),
		).Run()
	} else {
		winShowDialog(title, body, btn, 0)
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
		winShowDialog(title, body, "知道了", seconds)
	}
}

// Windows 11 风格 WPF 弹窗（圆角 + 阴影 + Segoe UI + 蓝色 Accent 按钮）
const winDialogXAML = `<Window xmlns="http://schemas.microsoft.com/winfx/2006/xaml/presentation" xmlns:x="http://schemas.microsoft.com/winfx/2006/xaml" Width="440" SizeToContent="Height" WindowStartupLocation="CenterScreen" ResizeMode="NoResize" WindowStyle="None" AllowsTransparency="True" Background="Transparent" Topmost="True" ShowInTaskbar="False" FontFamily="Segoe UI Variable Text, Segoe UI" FontSize="14">
  <Border Background="#F3F3F3" CornerRadius="8" BorderBrush="#30000000" BorderThickness="1" Margin="16">
    <Border.Effect><DropShadowEffect BlurRadius="24" ShadowDepth="2" Opacity="0.28" Color="#000000"/></Border.Effect>
    <Grid>
      <Grid.RowDefinitions><RowDefinition Height="Auto"/><RowDefinition Height="Auto"/></Grid.RowDefinitions>
      <StackPanel Grid.Row="0" Margin="28,24,28,22">
        <TextBlock x:Name="TitleText" FontSize="20" FontWeight="SemiBold" Foreground="#1C1C1C" TextWrapping="Wrap"/>
        <TextBlock x:Name="BodyText" Margin="0,12,0,0" FontSize="14" Foreground="#383838" TextWrapping="Wrap" LineHeight="22"/>
      </StackPanel>
      <Border Grid.Row="1" Background="#ECECEC" CornerRadius="0,0,8,8" Padding="24,16">
        <StackPanel Orientation="Horizontal" HorizontalAlignment="Right">
          <Button x:Name="OkBtn" MinWidth="108" Height="34" Foreground="White" BorderThickness="0" Cursor="Hand" FontWeight="SemiBold">
            <Button.Template>
              <ControlTemplate TargetType="Button">
                <Border x:Name="Bd" Background="#0067C0" CornerRadius="4">
                  <ContentPresenter HorizontalAlignment="Center" VerticalAlignment="Center"/>
                </Border>
                <ControlTemplate.Triggers>
                  <Trigger Property="IsMouseOver" Value="True"><Setter TargetName="Bd" Property="Background" Value="#1976D2"/></Trigger>
                  <Trigger Property="IsPressed" Value="True"><Setter TargetName="Bd" Property="Background" Value="#005BA6"/></Trigger>
                </ControlTemplate.Triggers>
              </ControlTemplate>
            </Button.Template>
          </Button>
        </StackPanel>
      </Border>
    </Grid>
  </Border>
</Window>`

const winInputXAML = `<Window xmlns="http://schemas.microsoft.com/winfx/2006/xaml/presentation" xmlns:x="http://schemas.microsoft.com/winfx/2006/xaml" Width="440" SizeToContent="Height" WindowStartupLocation="CenterScreen" ResizeMode="NoResize" WindowStyle="None" AllowsTransparency="True" Background="Transparent" Topmost="True" ShowInTaskbar="False" FontFamily="Segoe UI Variable Text, Segoe UI" FontSize="14">
  <Border Background="#F3F3F3" CornerRadius="8" BorderBrush="#30000000" BorderThickness="1" Margin="16">
    <Border.Effect><DropShadowEffect BlurRadius="24" ShadowDepth="2" Opacity="0.28" Color="#000000"/></Border.Effect>
    <Grid>
      <Grid.RowDefinitions><RowDefinition Height="Auto"/><RowDefinition Height="Auto"/></Grid.RowDefinitions>
      <StackPanel Grid.Row="0" Margin="28,24,28,22">
        <TextBlock x:Name="TitleText" FontSize="20" FontWeight="SemiBold" Foreground="#1C1C1C"/>
        <TextBlock x:Name="PromptText" Margin="0,10,0,14" FontSize="14" Foreground="#383838" TextWrapping="Wrap"/>
        <Border Background="White" CornerRadius="4" BorderBrush="#BDBDBD" BorderThickness="1">
          <TextBox x:Name="Input" BorderThickness="0" Background="Transparent" Padding="10,8" FontSize="15" VerticalContentAlignment="Center"/>
        </Border>
      </StackPanel>
      <Border Grid.Row="1" Background="#ECECEC" CornerRadius="0,0,8,8" Padding="24,16">
        <StackPanel Orientation="Horizontal" HorizontalAlignment="Right">
          <Button x:Name="CancelBtn" Content="取消" MinWidth="96" Height="34" Margin="0,0,10,0" Foreground="#1C1C1C" BorderThickness="1" Cursor="Hand">
            <Button.Template>
              <ControlTemplate TargetType="Button">
                <Border x:Name="Bd" Background="#FDFDFD" BorderBrush="#D0D0D0" BorderThickness="1" CornerRadius="4">
                  <ContentPresenter HorizontalAlignment="Center" VerticalAlignment="Center"/>
                </Border>
                <ControlTemplate.Triggers>
                  <Trigger Property="IsMouseOver" Value="True"><Setter TargetName="Bd" Property="Background" Value="#F5F5F5"/></Trigger>
                </ControlTemplate.Triggers>
              </ControlTemplate>
            </Button.Template>
          </Button>
          <Button x:Name="OkBtn" Content="确定" MinWidth="96" Height="34" Foreground="White" BorderThickness="0" Cursor="Hand" FontWeight="SemiBold" IsDefault="True">
            <Button.Template>
              <ControlTemplate TargetType="Button">
                <Border x:Name="Bd" Background="#0067C0" CornerRadius="4">
                  <ContentPresenter HorizontalAlignment="Center" VerticalAlignment="Center"/>
                </Border>
                <ControlTemplate.Triggers>
                  <Trigger Property="IsMouseOver" Value="True"><Setter TargetName="Bd" Property="Background" Value="#1976D2"/></Trigger>
                  <Trigger Property="IsPressed" Value="True"><Setter TargetName="Bd" Property="Background" Value="#005BA6"/></Trigger>
                </ControlTemplate.Triggers>
              </ControlTemplate>
            </Button.Template>
          </Button>
        </StackPanel>
      </Border>
    </Grid>
  </Border>
</Window>`

func winShowDialog(title, body, btn string, timeoutSec int) {
	script := `Add-Type -AssemblyName PresentationFramework,PresentationCore,WindowsBase
[xml]$x = $env:MH_XAML
$r = New-Object System.Xml.XmlNodeReader $x
$w = [Windows.Markup.XamlReader]::Load($r)
$w.Title = $env:MH_TITLE
$w.FindName('TitleText').Text = $env:MH_TITLE
$w.FindName('BodyText').Text = $env:MH_BODY
$ok = $w.FindName('OkBtn'); $ok.Content = $env:MH_BTN
$ok.Add_Click({ $w.Close() })
$w.Add_MouseLeftButtonDown({ try { $w.DragMove() } catch {} })
$w.Add_KeyDown({ if ($_.Key -eq 'Escape' -or $_.Key -eq 'Enter') { $w.Close() } })
$t = [int]$env:MH_TIMEOUT
if ($t -gt 0) {
  $tm = New-Object System.Windows.Threading.DispatcherTimer
  $tm.Interval = [TimeSpan]::FromSeconds($t)
  $tm.Add_Tick({ $tm.Stop(); $w.Close() })
  $tm.Start()
}
$w.ShowDialog() | Out-Null`
	cmd := exec.Command("powershell", "-NoProfile", "-ExecutionPolicy", "Bypass", "-WindowStyle", "Hidden", "-Command", script)
	cmd.Env = append(os.Environ(),
		"MH_XAML="+winDialogXAML,
		"MH_TITLE="+title,
		"MH_BODY="+body,
		"MH_BTN="+btn,
		fmt.Sprintf("MH_TIMEOUT=%d", timeoutSec),
	)
	cmd.Run()
}

func winShowInput(title, prompt, defaultVal string) string {
	script := `Add-Type -AssemblyName PresentationFramework,PresentationCore,WindowsBase
[xml]$x = $env:MH_XAML
$r = New-Object System.Xml.XmlNodeReader $x
$w = [Windows.Markup.XamlReader]::Load($r)
$w.Title = $env:MH_TITLE
$w.FindName('TitleText').Text = $env:MH_TITLE
$w.FindName('PromptText').Text = $env:MH_PROMPT
$in = $w.FindName('Input'); $in.Text = $env:MH_DEFAULT
$in.Loaded.Add({ $in.SelectAll(); $in.Focus() })
$script:result = ''
$w.FindName('OkBtn').Add_Click({ $script:result = $in.Text; $w.Close() })
$w.FindName('CancelBtn').Add_Click({ $w.Close() })
$w.Add_MouseLeftButtonDown({ try { $w.DragMove() } catch {} })
$w.Add_KeyDown({ if ($_.Key -eq 'Escape') { $w.Close() } })
$w.ShowDialog() | Out-Null
[Console]::Out.Write($script:result)`
	cmd := exec.Command("powershell", "-NoProfile", "-ExecutionPolicy", "Bypass", "-WindowStyle", "Hidden", "-Command", script)
	cmd.Env = append(os.Environ(),
		"MH_XAML="+winInputXAML,
		"MH_TITLE="+title,
		"MH_PROMPT="+prompt,
		"MH_DEFAULT="+defaultVal,
	)
	out, err := cmd.Output()
	if err != nil {
		return ""
	}
	return string(out)
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
		result = winShowInput("设置时间", prompt, currentVal)
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
	png := rgbaToPNG(w, h, rgba)
	if runtime.GOOS == "windows" {
		return pngToICO(png, w, h)
	}
	return png
}

// ICO 容器（直接内嵌 PNG）
func pngToICO(png []byte, w, h int) []byte {
	out := make([]byte, 0, 22+len(png))
	// ICONDIR: reserved(2)=0, type(2)=1, count(2)=1
	out = append(out, 0, 0, 1, 0, 1, 0)
	// ICONDIRENTRY
	bw := byte(0) // 0 means 256
	if w < 256 { bw = byte(w) }
	bh := byte(0)
	if h < 256 { bh = byte(h) }
	size := uint32(len(png))
	offset := uint32(22)
	out = append(out,
		bw, bh,
		0,    // color count
		0,    // reserved
		1, 0, // planes
		32, 0, // bpp
		byte(size), byte(size >> 8), byte(size >> 16), byte(size >> 24),
		byte(offset), byte(offset >> 8), byte(offset >> 16), byte(offset >> 24),
	)
	out = append(out, png...)
	return out
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
