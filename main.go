package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"image"
	"image/color"
	"image/png"
	"log"
	"math"
	"os"
	"path/filepath"
	"runtime"
	"strings"

	"github.com/egoist/mygo"
	"github.com/egoist/mygo/plugins/permissionflow"
	"github.com/egoist/mygo/ui"

	"translate/internal/selectionmonitor"
	"translate/internal/translation"
	"translate/modules/selection"
)

const (
	appName      = "划词翻译"
	defaultModel = "deepseek-flash"
	secretKey    = "llm-api-key"
)

type app struct {
	popup, settings *mygo.Window
	tray            *mygo.Tray
	monitor         selection.Monitor
	monitorStarted  bool
	cache           *translation.SQLiteCache
	service         *translation.Service
	dataDir         string
	config, draft   translation.Config
	loaded          bool
	status          string
	result          string
	resultTitle     string
	loading         bool
	requestID       uint64
	quitting        bool
}

func (a *app) resultView(c *ui.Context) {
	t := c.Theme()
	ui.Column(c).Fill().Background(t.Background).Children(func() {
		ui.Row(c).Padding(12, 16).Gap(10).AlignItems(ui.Center).Children(func() {
			ui.Text(c, "译").Bold().TextColor(t.TextMuted)
			ui.Text(c, a.resultTitle).FontSize(14).Bold()
		})
		ui.Divider(c)
		if a.loading {
			ui.Row(c).Padding(12, 16).Gap(10).AlignItems(ui.Center).Children(func() {
				ui.Spinner(c).Label("正在翻译")
				ui.Text(c, "正在翻译…").FontSize(14).TextColor(t.TextMuted)
			})
		} else {
			ui.Scroll(c).Grow(1).Padding(12, 16).Selectable().Children(func() {
				ui.Text(c, a.result).FontSize(14)
			})
		}
	})
}

func (a *app) settingsView(c *ui.Context) {
	a.settingsViewForOS(c, runtime.GOOS)
}

func (a *app) settingsViewForOS(c *ui.Context, goos string) {
	t := c.Theme()
	ui.Scroll(c).Fill().Children(func() {
		ui.Column(c).Padding(24).Gap(12).Children(func() {
			ui.Text(c, appName).FontSize(22).Bold()
			ui.Text(c, "选中文本后，按住鼠标右键 0.5 秒即可翻译。").TextColor(t.TextMuted)
			if goos == "darwin" {
				ui.Text(c, "触控板可用两指按下并保持 0.5 秒。\n请在系统设置中启用双指辅助点按。").TextColor(t.TextMuted)
			}
			ui.Text(c, "Base URL").Bold()
			ui.TextInput(c, &a.draft.BaseURL).Placeholder("例如 https://api.example.com/v1").Label("Base URL").Width(450)
			ui.Text(c, "模型").Bold()
			ui.TextInput(c, &a.draft.Model).Label("模型").Width(450)
			ui.Text(c, "API Key").Bold()
			ui.TextInput(c, &a.draft.APIKey).Password().Label("API Key").Width(450)
			if ui.PrimaryButton(c, "保存设置").Clicked() {
				a.saveSettings()
			}
			ui.Divider(c)
			if goos == "darwin" {
				ui.Text(c, "系统权限").Bold()
				ui.Text(c, "监听鼠标右键和模拟 ⌘C 需要辅助功能权限；系统要求时还需输入监控权限。").TextColor(t.TextMuted)
				ui.Row(c).Gap(8).Children(func() {
					if ui.Button(c, "辅助功能授权").Clicked() {
						a.guidePermission(permissionflow.Accessibility)
					}
					if ui.Button(c, "输入监控授权").Clicked() {
						a.guidePermission(permissionflow.InputMonitoring)
					}
				})
			} else if goos == "windows" {
				ui.Text(c, "选区读取").Bold()
				ui.Text(c, "应用通过模拟 Ctrl+C 读取选区，剪贴板会被替换。").TextColor(t.TextMuted)
			}
			if a.status != "" {
				ui.Text(c, a.status).TextColor(t.TextMuted)
			}
		})
	})
}

func (a *app) ready() {
	var err error
	a.dataDir, err = mygo.App.Path(mygo.PathUserData)
	if err != nil {
		log.Printf("无法打开应用数据目录：%v", err)
		mygo.App.Quit()
		return
	}
	a.popup = mygo.NewWindow(mygo.WindowOptions{
		Title: appName, Panel: true, Hidden: true, Frameless: true,
		Transparent: true, CornerRadius: 12, DisableResize: true,
		Width: 380, Height: 210, Content: ui.View(a.resultView),
	})
	a.settings = mygo.NewWindow(mygo.WindowOptions{
		Title: appName + "设置", Hidden: true, Width: 520, Height: 620,
		MinWidth: 500, MinHeight: 480, Content: ui.View(a.settingsView),
	})
	a.popup.OnBlur(func() { a.popup.Hide() })
	a.popup.OnClose(func(e *mygo.CloseEvent) {
		if !a.quitting {
			e.PreventDefault()
			a.popup.Hide()
		}
	})
	a.popup.OnHide(func() { a.requestID++; a.refreshAreas() })
	a.popup.OnShow(a.refreshAreas)
	a.popup.OnMove(a.refreshAreas)
	a.popup.OnResize(a.refreshAreas)
	a.settings.OnClose(func(e *mygo.CloseEvent) {
		if !a.quitting {
			e.PreventDefault()
			a.settings.Hide()
		}
	})
	a.settings.OnShow(a.refreshAreas)
	a.settings.OnHide(a.refreshAreas)
	a.settings.OnMove(a.refreshAreas)
	a.settings.OnResize(a.refreshAreas)

	a.monitor, err = selectionmonitor.New()
	if err != nil {
		a.status = "无法启动选区模块：" + err.Error()
	}
	a.createTray()
	a.draft.Model = defaultModel
	go a.load()
}

func (a *app) createTray() {
	tray, err := mygo.NewTray(mygo.TrayOptions{Icon: trayIcon(), IconIsTemplate: true, ToolTip: appName})
	if err != nil {
		a.status = "无法创建菜单栏图标：" + err.Error()
		return
	}
	a.tray = tray
	tray.SetMenu(mygo.NewMenu([]*mygo.MenuItem{
		{Label: "设置", Click: func(*mygo.MenuItem, *mygo.Window) { a.showSettings() }},
		mygo.Separator(),
		{Label: "退出", Click: func(*mygo.MenuItem, *mygo.Window) { mygo.App.Quit() }},
	}))
}

func (a *app) showSettings() {
	a.settings.Show()
	a.settings.Focus()
	a.refreshAreas()
}

func area(w *mygo.Window) selection.Area {
	if w == nil || !w.IsVisible() {
		return selection.Area{}
	}
	b := selectionWindowBounds(w)
	return selection.Area{X: b.X, Y: b.Y, Width: b.Width, Height: b.Height, Visible: true}
}

func (a *app) refreshAreas() {
	if a.monitor != nil {
		a.monitor.SetAreas(area(a.popup), area(a.settings))
	}
}

func (a *app) load() {
	cfg := translation.Config{Model: defaultModel}
	var issues []string
	data, err := os.ReadFile(filepath.Join(a.dataDir, "settings.json"))
	if err == nil {
		if err := json.Unmarshal(data, &cfg); err != nil {
			issues = append(issues, "读取设置失败："+err.Error())
		}
	} else if !errors.Is(err, os.ErrNotExist) {
		issues = append(issues, "读取设置失败："+err.Error())
	}
	key, err := mygo.Secrets.Get(secretKey)
	if err == nil {
		cfg.APIKey = string(key)
	} else if !errors.Is(err, mygo.ErrSecretNotFound) {
		label := "读取钥匙串失败："
		if runtime.GOOS == "windows" {
			label = "读取 Windows 凭据失败："
		}
		issues = append(issues, label+err.Error())
	}
	cache, err := translation.OpenSQLiteCache(context.Background(), filepath.Join(a.dataDir, "translations.sqlite"))
	if err != nil {
		issues = append(issues, "打开缓存失败："+err.Error())
	}
	a.settings.Update(func() {
		a.config = cfg
		a.draft = cfg
		a.loaded = true
		if cache != nil {
			a.cache = cache
			a.service = translation.NewService(cache, nil)
		}
		if len(issues) > 0 {
			a.status = strings.Join(issues, "\n")
		}
		a.startMonitor()
		if cfg.BaseURL == "" || !a.monitorStarted || len(issues) > 0 {
			a.showSettings()
		}
	})
}

func (a *app) saveSettings() {
	if !a.loaded {
		a.status = "正在读取设置，请稍候。"
		return
	}
	cfg := a.draft
	a.status = "正在保存设置…"
	go func() {
		err := mygo.Secrets.Set(secretKey, []byte(cfg.APIKey))
		if err == nil {
			var data []byte
			data, err = json.MarshalIndent(cfg, "", "  ")
			if err == nil {
				err = os.WriteFile(filepath.Join(a.dataDir, "settings.json"), data, 0o600)
			}
		}
		a.settings.Update(func() {
			if err != nil {
				a.status = "保存设置失败：" + err.Error()
				return
			}
			a.config = cfg
			a.status = "设置已保存。"
			a.startMonitor()
		})
	}()
}

func (a *app) startMonitor() {
	if a.monitor == nil || a.monitorStarted {
		return
	}
	if runtime.GOOS == "darwin" {
		status, err := permissionflow.Check(permissionflow.Accessibility)
		if err != nil || status != permissionflow.Granted {
			a.status = "请先授予辅助功能权限，然后返回应用。"
			return
		}
	}
	if err := a.monitor.Start(a.translateSelection, func() { a.popup.Hide() }, func() {
		a.monitorStarted = false
		a.monitor.Stop()
		a.status = "鼠标监听已停止，请检查系统权限并重新授权。"
		if runtime.GOOS == "windows" {
			a.status = "鼠标监听已停止，请重新打开应用。"
		}
		a.settings.Invalidate()
	}); err != nil {
		a.status = "无法监听鼠标，请检查辅助功能和输入监控权限：" + err.Error()
		if runtime.GOOS == "windows" {
			a.status = "无法监听鼠标：" + err.Error()
		}
		return
	}
	a.monitorStarted = true
	a.refreshAreas()
	if a.status == "" || a.status == "请先授予辅助功能权限，然后返回应用。" {
		a.status = "已就绪。"
	}
}

func (a *app) guidePermission(permission permissionflow.Permission) {
	go func() {
		status, err := permissionflow.Guide(context.Background(), permission, permissionflow.GuideOptions{})
		a.settings.Update(func() {
			if err != nil {
				a.status = "打开权限设置失败：" + err.Error()
			} else if status == permissionflow.Granted {
				a.status = "权限已授予。"
				a.startMonitor()
			} else {
				a.status = "尚未授予权限。"
			}
		})
	}()
}

func (a *app) translateSelection() {
	a.requestID++
	id := a.requestID
	if !a.loaded || a.service == nil {
		a.present("无法翻译", "应用设置或缓存尚未就绪，请打开设置查看。", false)
		return
	}
	cfg, svc, monitor := a.config, a.service, a.monitor
	if cfg.BaseURL == "" {
		a.present("无法翻译", "请先在设置中填写 Base URL。", false)
		return
	}
	// Showing without taking focus preserves the selection in the source app.
	a.present("翻译中", "", true)
	go func() {
		text, err := monitor.SelectedText()
		var result string
		if err == nil {
			result, err = svc.Translate(context.Background(), cfg, text)
		}
		if errors.Is(err, selection.ErrNoSelection) {
			err = errors.New("无法复制选中的文本。请确认已选择文本，且当前应用支持复制。")
		} else if errors.Is(err, selection.ErrNoPermission) {
			err = errors.New("请在设置中授予辅助功能权限。")
		}
		a.popup.Update(func() {
			if id != a.requestID {
				return
			}
			a.loading = false
			if err != nil {
				a.resultTitle, a.result = "无法翻译", err.Error()
			} else {
				a.resultTitle, a.result = "翻译结果", result
			}
			// Keep the position chosen when the gesture fired, but let the
			// result take focus so its text can be selected and copied.
			a.popup.Show()
		})
	}()
}

func (a *app) present(title, text string, loading bool) {
	a.resultTitle, a.result, a.loading = title, text, loading
	point := mygo.Screen.CursorScreenPoint()
	work := mygo.Screen.DisplayNearestPoint(point).WorkArea
	w, h := a.popup.Size()
	x := max(work.X, min(point.X+12, max(work.X, work.X+work.Width-w)))
	y := max(work.Y, min(point.Y+12, max(work.Y, work.Y+work.Height-h)))
	a.popup.SetPosition(x, y)
	if loading {
		a.popup.ShowInactive()
	} else {
		a.popup.Show()
	}
	a.refreshAreas()
	a.popup.Invalidate()
}

func trayIcon() []byte {
	// A small 文/A mark, black on transparent so macOS can tint it for the
	// current menu bar. Supersampling keeps the strokes smooth at 18 points.
	type line struct{ x1, y1, x2, y2 float64 }
	strokes := []line{
		{9, 3, 10, 5}, {3, 7, 18, 7},
		{5, 9, 8, 13}, {8, 13, 12, 16}, {12, 16, 17, 18},
		{16, 8, 14, 12}, {14, 12, 11, 15}, {11, 15, 4, 18},
		{17, 28, 23.5, 14.5}, {23.5, 14.5, 30, 28}, {19.5, 23, 27.5, 23},
	}
	img := image.NewNRGBA(image.Rect(0, 0, 32, 32))
	const samples = 4
	for y := range 32 {
		for x := range 32 {
			covered := 0
			for sy := range samples {
				for sx := range samples {
					px := float64(x) + (float64(sx)+0.5)/samples
					py := float64(y) + (float64(sy)+0.5)/samples
					for _, s := range strokes {
						dx, dy := s.x2-s.x1, s.y2-s.y1
						at := max(0, min(1, ((px-s.x1)*dx+(py-s.y1)*dy)/(dx*dx+dy*dy)))
						if math.Hypot(px-(s.x1+at*dx), py-(s.y1+at*dy)) <= 1.1 {
							covered++
							break
						}
					}
				}
			}
			img.SetNRGBA(x, y, color.NRGBA{A: uint8(covered * 255 / (samples * samples))})
		}
	}
	var out bytes.Buffer
	_ = png.Encode(&out, img)
	return out.Bytes()
}

func main() {
	if !mygo.App.RequestSingleInstanceLock() {
		return
	}
	a := &app{}
	mygo.App.SetName(appName)
	mygo.App.SetActivationPolicy(mygo.ActivationPolicyAccessory)
	mygo.App.OnWindowAllClosed(func() {})
	mygo.App.OnSecondInstance(func([]string, string) { a.showSettings() })
	mygo.App.WhenReady(a.ready)
	mygo.App.OnBeforeQuit(func(*mygo.QuitEvent) { a.quitting = true })
	mygo.App.OnQuitCanceled(func() { a.quitting = false })
	mygo.App.OnWillQuit(func(*mygo.QuitEvent) {
		if a.monitor != nil {
			a.monitor.Stop()
		}
		if a.cache != nil {
			_ = a.cache.Close()
		}
		if a.tray != nil {
			a.tray.Destroy()
		}
	})
	if err := mygo.App.Run(); err != nil {
		log.Fatal(err)
	}
}
