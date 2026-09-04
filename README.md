# DSH Desktop

<p align="center">
  <img src="internal/appicon/dsh-desktop-icon.png" alt="DSH Desktop icon" width="192">
</p>

DSH Desktop 是基于 Go 和 Wails 构建的 DeepSeek DSH 桌面客户端。它会启动本机 DSH Web 服务，并通过系统 WebView 提供桌面窗口和托盘运行体验。

运行前请安装带有 `npx` 的 Node.js；如果同时安装了 [Bun](https://bun.sh)，应用会优先使用 `bunx`，找不到 `bunx` 时自动回退到 `npx`。

每次启动或重启 DSH 时，应用都会先查询 npm registry 的 `latest` dist-tag，取得精确版本号，然后执行：

```text
bunx @deepseek-ai/dsh@<version> web --no-open
```

回退到 Node.js 时会执行等价的 `npx` 命令。可通过 `DSH_NPM_REGISTRY` 指定 registry；未设置时依次使用 `NPM_CONFIG_REGISTRY` 和 npm 官方 registry。

DSH 输出带认证 token 的启动地址时，应用会自动用它建立 WebView 会话。token 只在内存中短暂使用，终端日志和启动页面只显示脱敏后的地址。

## 运行环境

macOS 和 Linux 会读取当前用户常见 shell 的环境配置，支持 zsh、bash、sh/dash/ksh 和 fish；Windows 直接使用系统环境变量，不执行 shell 配置文件。解析后的同一份环境会同时用于定位 bunx、npx、Node.js 和启动 DSH。

路径按以下优先级解析：

1. 工具专用变量，例如 `DSH_BUNX_PATH`、`DSH_NPX_PATH`、`DSH_NODE_PATH`、`BUN_INSTALL`、`NPM_CONFIG_PREFIX`、`NODE_HOME`、`NVM_BIN`、`VOLTA_HOME` 和 `DSH_HOME`。
2. XDG 变量，例如 `XDG_BIN_HOME`、`XDG_DATA_HOME` 和 `XDG_CONFIG_HOME`。
3. `PATH` 以及各操作系统的常见默认安装路径。

如果 `DSH_HOME` 未显式设置，但 `$XDG_CONFIG_HOME/dsh` 或默认的 `~/.config/dsh` 已存在，应用会自动设置 `DSH_HOME`。
