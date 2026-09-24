# DSH Desktop

<p align="center">
  <img src="internal/appicon/dsh-desktop-icon.png" alt="DSH Desktop icon" width="192">
</p>

DSH Desktop 是基于 Go 和 Wails 构建的 DeepSeek DSH 桌面客户端。它会启动本机 DSH Web 服务，并通过系统 WebView 提供桌面窗口和托盘运行体验。

运行前请安装带有 `npx` 的 Node.js；如果同时安装了 [Bun](https://bun.sh)，应用会优先使用 `bunx`，找不到 `bunx` 时自动回退到 `npx`。

每次启动或重启 DSH 时，应用都会查询 npm registry 已发布的版本，选择语义化版本最高的一个（包含预发布版本，不使用可能滞后的 `latest` dist-tag），然后执行：

```text
bunx @deepseek-ai/dsh@<version> web --no-open --port <port>
```

回退到 Node.js 时会执行等价的 `npx` 命令。可通过 `DSH_NPM_REGISTRY` 指定 registry；未设置时依次使用 `NPM_CONFIG_REGISTRY` 和 npm 官方 registry。

DSH 输出带认证 token 的启动地址时，应用会自动用它建立 WebView 会话。token 只在内存中短暂使用，终端日志和启动页面只显示脱敏后的地址。

启动页显示当前阶段和无边框的实时启动记录，包含启动命令、端口和认证信息。时间位于右侧，最新记录高亮淡入，旧记录弱化并在顶部渐隐；日志区域保持固定高度，翻阅或选择文字时暂停跟随，可点击“新日志 ↓”回到底部。单个阶段等待超过 10 秒时会显示已等待时间，失败时可以直接重试，日志可随时复制且已脱敏。动画遵循系统“减少动态效果”设置，不影响认证就绪后立即进入 DSH 页面。

启动和重启时，应用先检查已有 DSH 进程（包括其他端口上的实例），再检查可用端口。当前地址上无需认证的稳定 DSH 服务可以直接复用；其他已识别实例会提示选择结束并重新启动，或保留它们并启动新实例，也可以取消。结束进程前会重新核对进程身份，不会按端口号结束未知程序。

新实例优先使用 3080（同一次运行中重启优先沿用上次端口），被占用时向后检查最多 50 个端口。端口检查使用实际 TCP 绑定，因此非 HTTP 程序占用端口也会被跳过。无界面 smoke test 使用相同检查流程，但始终保留外部 DSH 进程。

## 运行环境

macOS 和 Linux 会读取当前用户常见 shell 的环境配置，支持 zsh、bash、sh/dash/ksh 和 fish；Windows 直接使用系统环境变量，不执行 shell 配置文件。解析后的同一份环境会同时用于定位 bunx、npx、Node.js 和启动 DSH。

路径按以下优先级解析：

1. 工具专用变量，例如 `DSH_BUNX_PATH`、`DSH_NPX_PATH`、`DSH_NODE_PATH`、`BUN_INSTALL`、`NPM_CONFIG_PREFIX`、`NODE_HOME`、`NVM_BIN`、`VOLTA_HOME` 和 `DSH_HOME`。
2. XDG 变量，例如 `XDG_BIN_HOME`、`XDG_DATA_HOME` 和 `XDG_CONFIG_HOME`。
3. `PATH` 以及各操作系统的常见默认安装路径。

如果 `DSH_HOME` 未显式设置，但 `$XDG_CONFIG_HOME/dsh` 或默认的 `~/.config/dsh` 已存在，应用会自动设置 `DSH_HOME`。

启动 `bunx` 时，如果 `TMP` 或 `TEMP` 未设置，应用会补齐缺失项。只缺其中一个时，使用另一个的路径；两个都没有时，Windows 使用 `%LOCALAPPDATA%\Temp`，否则使用 `%USERPROFILE%\AppData\Local\Temp`；macOS 和 Linux 优先使用 `TMPDIR`，没有则使用 `/tmp`。
