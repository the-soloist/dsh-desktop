# DSH Desktop

DSH Desktop 是一个使用 Go 和 Wails 编写的 DeepSeek DSH 桌面客户端。程序先启动本机 DSH Web 服务，再通过系统 WebView 打开 `http://127.0.0.1:3080`；安装包不包含 Chromium、Bun 或 DSH。

## 启动流程

1. 解析 `$XDG_CONFIG_HOME/dsh`；未设置 `XDG_CONFIG_HOME` 时使用 `~/.config/dsh`。目录存在时，以 `DSH_HOME` 指向该目录启动 DSH。
2. 查找 `bunx`（支持通过 `DSH_BUNX_PATH` 指定）。找不到时显示系统原生警告并退出，不会回退到 `npx`。
3. 如果 `127.0.0.1:3080` 已有可访问的服务则直接复用，否则执行：

   ```bash
   bunx @deepseek-ai/dsh@latest web --no-open
   ```

4. DSH 就绪后创建桌面窗口；Windows 从资源管理器启动时会先显示控制台，就绪后自动隐藏。若从已有终端启动，不会隐藏用户的终端窗口。
5. 窗口关闭时保存正常状态下的位置、大小和最大化状态，并终止本次程序启动的 DSH 进程树；复用的外部 DSH 服务不会被终止。

窗口状态保存在系统用户配置目录下的 `DshDesktop/window-state.json`。

## 系统要求

- Go 1.25 或更高版本（仅构建时需要）
- Bun，并确保 `bunx` 可执行
- macOS：系统 WebKit 和 Xcode Command Line Tools
- Linux：GTK 4、WebKitGTK 6.0、`pkg-config`、`appimagetool` 和 7-Zip
- Windows：系统 WebView2 和 7-Zip

首次运行 `bunx @deepseek-ai/dsh@latest` 时可能需要联网下载 DSH。目前固定使用 Wails `v3.0.0-beta.16`；应用只加载本机 URL，不向网页暴露 Go 服务绑定。

## 构建

每个平台都在对应操作系统上执行同一个脚本，不进行交叉编译：

```bash
bun run build.ts
```

所有中间文件位于 `dist/intermediate/<os>`，最终结果统一命名为 `DshDesktop-<os>-<arch>.7z`：

- `dist/macos/DshDesktop-macos-x86_64.7z`
- `dist/macos/DshDesktop-macos-arm64.7z`
- `dist/linux/DshDesktop-linux-x86_64.7z`
- `dist/windows/DshDesktop-windows-x86_64.7z`

macOS 压缩包内是经过 ad-hoc 签名的 `DshDesktop.app`；正式发布仍需 Developer ID 签名和公证。Linux 压缩包内是 `DshDesktop.AppImage`，运行时仍依赖系统 WebKitGTK。Windows 压缩包内是便携版 `DshDesktop.exe`，不生成安装程序。

可选的启动测试：

```bash
bun run build.ts --smoke-test
```

## CI

`.github/workflows/ci.yml` 使用 matrix 在原生 runner 上构建：

- macOS x86_64
- macOS arm64
- Linux x86_64
- Windows x86_64

各系统依赖检查分别位于规范命名的 `Dependencies | <OS>` step；编译统一使用 `Build application`，产物统一使用 `Upload application` 上传。

## 运行参数

- `DSH_WORKSPACE`：DSH 工作目录，默认为用户主目录。
- `DSH_BUNX_PATH`：`bunx` 的绝对路径。
- `DSH_START_TIMEOUT_SECONDS`：等待 DSH 就绪的秒数，默认 300。
- `DSH_SMOKE_TEST_SECONDS`：启动测试保持窗口打开的秒数，默认 8。
- `DSH_LAUNCHER_LOG`：覆盖启动日志路径。

默认日志位置：

- macOS：`~/Library/Logs/DshDesktop/launcher.log`
- Linux：`$XDG_STATE_HOME/dsh-desktop/launcher.log`，默认 `~/.local/state/dsh-desktop/launcher.log`
- Windows：`%LOCALAPPDATA%\DshDesktop\launcher.log`
