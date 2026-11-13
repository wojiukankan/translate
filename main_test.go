package main

import (
	"testing"
	"time"

	"github.com/egoist/mygo/ui"
)

func TestView(t *testing.T) {
	a := &app{resultTitle: "翻译中", result: "旧译文", loading: true}
	tt := ui.NewTester(a.resultView, 380, 210)
	if !tt.HasText("翻译中") || !tt.HasText("正在翻译…") || tt.HasText("旧译文") {
		t.Fatalf("loading view texts = %q", tt.Texts())
	}
	if _, ok := tt.Find("正在翻译"); !ok {
		t.Fatal("loading indicator is missing")
	}
	tt.Advance(time.Second)
	a.loading = false
	a.resultTitle, a.result = "翻译结果", "你好，世界"
	tt.Frame()
	if !tt.HasText("翻译结果") || !tt.HasText("你好，世界") {
		t.Fatalf("result view texts = %q", tt.Texts())
	}
	if tt.HasText("正在翻译") {
		t.Fatalf("loading indicator remains in result view: %q", tt.Texts())
	}
	if err := tt.RightClick("你好，世界"); err != nil {
		t.Fatal(err)
	}
	if err := tt.ChooseMenuItem("Select All"); err != nil {
		t.Fatal(err)
	}
	if err := tt.RightClick("你好，世界"); err != nil {
		t.Fatal(err)
	}
	if err := tt.ChooseMenuItem("Copy"); err != nil {
		t.Fatal(err)
	}
	if got := tt.Clipboard(); got != "你好，世界" {
		t.Fatalf("copied %q", got)
	}
	a.loading = true
	a.resultTitle, a.result = "翻译中", ""
	tt.Frame()
	a.loading = false
	a.resultTitle, a.result = "无法翻译", "请求失败"
	tt.Frame()
	if !tt.HasText("无法翻译") || !tt.HasText("请求失败") || tt.HasText("正在翻译") {
		t.Fatalf("error view texts = %q", tt.Texts())
	}

	for _, tc := range []struct {
		os, want, absent string
	}{
		{"darwin", "辅助功能授权", "模拟 Ctrl+C"},
		{"windows", "应用通过模拟 Ctrl+C 读取选区，剪贴板会被替换。", "辅助功能授权"},
	} {
		t.Run(tc.os+" settings", func(t *testing.T) {
			settings := ui.NewTester(func(c *ui.Context) { a.settingsViewForOS(c, tc.os) }, 520, 620)
			if !settings.HasText(tc.want) || settings.HasText(tc.absent) {
				t.Fatalf("settings texts = %q", settings.Texts())
			}
		})
	}
}
