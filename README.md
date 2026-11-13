# 划词翻译

一个驻留在 macOS 菜单栏或 Windows 系统托盘的 MyGo 应用。选中文本后按住鼠标右键 0.5 秒，鼠标附近立即显示翻译中的浮窗，选区发送给配置的 LLM 翻译成中文。完成后浮窗显示译文，可以选择文字后按 ⌘C（macOS）、Ctrl+C（Windows）或使用右键菜单复制；点击其他位置关闭浮窗。普通右键在松开后打开原应用的菜单。

下载地址：[GitHub Releases](https://github.com/wojiukankan/translate/releases)。

## macOS 使用

1. 下载并打开 macOS 通用版 DMG，将“划词翻译”拖入应用程序。支持 macOS 13 及以上的 Apple 芯片和 Intel Mac。
2. 在设置中填写 OpenAI 兼容服务的 Base URL、模型和 API Key，点击“保存设置”。例如 DeepSeek 官方的 Base URL 为 `https://api.deepseek.com`，应用会在其后追加 `/chat/completions`。默认模型为 `deepseek-flash`。
3. 点击“辅助功能授权”，按系统提示允许本应用访问辅助功能。如果系统还要求输入监控权限，点击“输入监控授权”。关闭设置窗口后，应用继续驻留菜单栏。
4. 在其他应用中选中文本，按住右键 0.5 秒。应用会模拟 ⌘C 复制选区，成功获取文本后，在能完整保存原剪贴板且剪贴板未被再次更改时尝试恢复；无法完整保存时会保留复制后的内容。

## Windows 使用

1. 解压新版 Windows 压缩包，双击运行 `划词翻译.exe`；应用驻留在任务栏通知区域。SQLite 已集成进程序，不再需要 `Sqlitecore.dll`。
2. 在设置中填写 OpenAI 兼容服务的 Base URL、模型和 API Key，点击“保存设置”。
3. 在其他应用中选中文本，按住鼠标右键 0.5 秒。应用会模拟 Ctrl+C 复制选区并替换剪贴板。Windows 对向高权限程序模拟输入有系统限制。

请求使用 `system: 翻译成中文`、选中的原文和 `reasoning_effort: none`。API Key 保存在 macOS 钥匙串或 Windows 凭据管理器；翻译结果保存在当前用户的应用数据目录，重启后相同原文和配置可直接命中缓存。接口地址或模型改变后，使用新的缓存条目。

macOS 应用使用临时签名，未经过 Apple 公证。重新构建、移动到其他位置或换一台 Mac 后，系统可能要求重新授予权限，其他 Mac 也可能被 Gatekeeper 拦截。Windows 构建未进行代码签名，首次运行时可能出现系统提示；Windows 版本尚未完成真机功能及杀毒软件验证。

## 开发与验证

需要 Go 1.27.4 或更高版本；构建 macOS 原生模块还需要 Swift 6 和 Xcode Command Line Tools。

```sh
go tool mygo native check ./modules/selection
CGO_ENABLED=0 go test ./...
go tool mygo vet .
CGO_ENABLED=0 go test -tags sqlitego ./internal/translation
go tool mygo build -platform darwin/universal,windows/amd64 -skip-installer -o build/release
python3 scripts/package_windows.py build/release/windows-amd64 build/release
```
