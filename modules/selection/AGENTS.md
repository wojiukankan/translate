# 选区和鼠标监听

- `selection.go` 定义跨平台共用的 `Monitor`、窗口区域和错误契约。
- macOS 实现在本目录的 `*_darwin.go` 中，通过 Go 和 `purego` 调用系统接口；Windows 实现在 `internal/selectionmonitor/`。这里不再是 MyGo 生成的原生模块，无需 Swift 或 `mygo native check`。
- macOS 事件监听及其状态在主线程处理，回调按顺序异步送回主线程；Windows 鼠标钩子运行在独立的消息循环线程。修改时保持回调顺序和线程边界，避免阻塞系统事件处理。
- 长按右键触发翻译，短按时回放被暂存的事件；应用自己的窗口区域内的右键直接交给窗口。两端的选区读取都通过模拟复制完成，macOS 仅在原剪贴板可以完整保存且未被再次更改时尝试恢复，Windows 会替换剪贴板。
- 改动时沿用现有测试并按影响验证：

  ```sh
  CGO_ENABLED=0 go test ./...
  go test -race ./modules/selection ./internal/selectionmonitor
  go tool mygo vet .
  ```

  平台相关的系统事件和剪贴板行为还需要在相应操作系统上验证。
