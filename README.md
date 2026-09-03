# DSH Desktop

<p align="center">
  <img src="internal/appicon/dsh-desktop-icon.png" alt="DSH Desktop icon" width="192">
</p>

DSH Desktop 是基于 Go 和 Wails 构建的 DeepSeek DSH 桌面客户端。它会启动本机 DSH Web 服务，并通过系统 WebView 提供桌面窗口和托盘运行体验。

运行前请安装 [Bun](https://bun.sh)，并确保 `bunx` 可用。

## 运行环境

macOS 和 Linux 会读取当前用户常见 shell 的环境配置，支持 zsh、bash、sh/dash/ksh 和 fish；Windows 直接使用系统环境变量，不执行 shell 配置文件。解析后的同一份环境会同时用于定位 bunx、Node.js 和启动 DSH。

路径按以下优先级解析：

1. 工具专用变量，例如 `DSH_BUNX_PATH`、`DSH_NODE_PATH`、`BUN_INSTALL`、`NODE_HOME`、`NVM_BIN`、`VOLTA_HOME` 和 `DSH_HOME`。
2. XDG 变量，例如 `XDG_BIN_HOME`、`XDG_DATA_HOME` 和 `XDG_CONFIG_HOME`。
3. `PATH` 以及各操作系统的常见默认安装路径。

如果 `DSH_HOME` 未显式设置，但 `$XDG_CONFIG_HOME/dsh` 或默认的 `~/.config/dsh` 已存在，应用会自动设置 `DSH_HOME`。
