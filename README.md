# DSH Desktop

<p align="center">
  <img src="internal/appicon/dsh-desktop-icon.png" alt="DSH Desktop icon" width="192">
</p>

DSH Desktop 是 [DeepSeek Harness（DSH）](https://github.com/deepseek-ai/deepseek-harness) 的轻量桌面版客户端，基于 Go 和 Wails 实现，不使用 Electron。

支持 macOS、Windows 和 Linux，自动启动本地 DSH 服务，提供桌面窗口、系统托盘与 Profile 切换功能。

## 运行要求

- 安装 Node.js，并确保 `node` 命令可用；即使使用 `bunx` 启动，也需要 Node.js。
- 确保 `bunx` 或 `npx` 命令可用。应用优先使用 `bunx`，未找到时回退到 `npx`，两者至少需要一个。安装 Bun 可提供 `bunx`，安装 Node.js 和 npm 可提供 `npx`。
- 将相关命令加入 `PATH`。启动时需要访问 npm registry 查询 DSH 版本，并按需下载依赖，请确保网络可用。

可在终端检查：

```sh
node --version

# 以下两项至少有一项可用
bunx --version
npx --version
```

如果终端中命令可用，但桌面应用仍无法找到，可通过 `DSH_BUNX_PATH`、`DSH_NPX_PATH`、`DSH_NODE_PATH` 环境变量指定对应可执行文件的完整路径，设置后重新启动应用。

## 开始使用

1. 从 [Releases](https://github.com/the-soloist/dsh-desktop/releases/latest) 下载适合操作系统和处理器架构的压缩包，解压后打开应用。
2. 应用会检查运行环境并启动本地 DSH 服务，无需手动运行 DSH 启动命令。
3. 可通过托盘菜单切换已有 Profile、重启 DSH 或退出应用。切换 Profile 会重启本应用管理的 DSH 服务，可能中断正在执行的任务。

DSH 本身的配置与使用方式请参阅 [上游项目文档](https://github.com/deepseek-ai/deepseek-harness#readme)。
