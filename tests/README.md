# 测试

运行跨平台测试：

```sh
bun install --frozen-lockfile
bun test tests
CGO_ENABLED=0 go test -tags server ./...
```

macOS 原生回归测试会打开临时窗口，使用临时状态文件：

```sh
go test -tags windowstatetest,layouttest ./internal/desktop -run 'TestNativeWindowState|TestLayoutWebView' -v
```

`layout-contract.jsx` 使用真实的生产版 React 渲染 DSH AppFrame 的适配契约，覆盖恢复、拖动保存、流式内容、组件重新挂载、刷新及不支持的布局结构。React 依赖仅用于测试。布局适配中的 DOM 标记和组件 actions 是兼容边界；上游变更时应更新夹具并运行原生测试。
