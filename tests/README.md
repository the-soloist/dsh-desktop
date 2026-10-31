# 测试

运行跨平台测试：

```sh
bun install --frozen-lockfile
bun test tests
CGO_ENABLED=0 go test -tags server ./...
```

macOS 原生回归测试会打开临时窗口，使用临时状态文件。窗口测试先读取屏幕工作区的逻辑尺寸，再生成两组可容纳的尺寸，复用于调整大小、重新打开及最大化恢复测试；日志包含屏幕、缩放倍率和预期尺寸：

```sh
go test -tags windowstatetest,layouttest ./internal/desktop -run 'TestNativeWindowState|TestLayoutWebView' -v
```

`layout-contract.jsx` 使用真实的生产版 React 渲染 DSH AppFrame 的适配契约，覆盖恢复、拖动保存、流式内容、组件重新挂载、刷新及不支持的布局结构。React 依赖仅用于测试。布局适配中的 DOM 标记和组件 actions 是兼容边界；上游变更时应更新夹具并运行原生测试。
