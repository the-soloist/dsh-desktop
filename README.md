# DSH Desktop launcher

This project wraps the Pake application with a small native Rust launcher. At
startup it:

1. detects `$XDG_CONFIG_HOME/dsh` (defaulting to `~/.config/dsh`) and, when it
   exists, exports it as `DSH_HOME`;
2. checks for `bunx`; if it is unavailable, it displays a platform warning and
   stops;
3. reuses an existing service on `127.0.0.1:3080`, or starts DSH;
4. waits until port 3080 is accepting connections;
5. starts the packaged Pake executable;
6. stops only the DSH process tree that it started when the Pake process exits.

The package intentionally does not bundle Bun, Node.js, or DSH. Bun and Node.js
must be available on the destination computer. The first run may use the
network to download the current DSH package.

## Build model

Pake does not cross-package desktop applications. Run the complete build on a
native runner for each operating system. The single build script invokes both
`bunx pake-cli` and the native Rust build, then assembles the platform package:

```bash
bun run build.ts
```

All project-level Pake, Cargo, extraction, and assembly intermediates are kept
under `dist/intermediate/<platform>`. Pass `--package-only` to reuse an existing
Pake intermediate, or `--smoke-test` to run a five-second startup check after
packaging. macOS supports x86_64 and arm64; Linux and Windows currently support
x86_64 only.

### macOS

```bash
bun run build.ts
```

Final packages (each containing `DshDesktop.app`):

- `dist/macos/DshDesktop-macos-x86_64.7z`
- `dist/macos/DshDesktop-macos-arm64.7z`

The script preserves the Pake `.app`, renames its executable to
`pake-dshdesktop-real`, installs the launcher at the original executable path,
and applies an ad-hoc signature. Developer ID distribution still requires a
production signature and notarization.

### Linux

```bash
APPIMAGETOOL=/absolute/path/to/appimagetool bun run build.ts
```

Final package: `dist/linux/DshDesktop-linux-x86_64.7z` (containing
`DshDesktop.AppImage`)

The script extracts the Pake AppImage, replaces the Pake entry executable, and
rebuilds the AppImage. `appimagetool` must be installed or supplied through
`APPIMAGETOOL`.

### Windows

```powershell
bun run build.ts
```

The script uses 7-Zip to produce
`dist/windows/DshDesktop-windows-x86_64.7z`, containing `DshDesktop.exe` and
the Pake payload. It does not generate an installer.
When launched by double-click, Windows shows a startup console with progress;
after DSH is ready and Pake remains running, the launcher hides that console.

## CI

`.github/workflows/ci.yml` runs formatting, Clippy, and tests, then builds on
native macOS, Linux, and Windows runners. Its matrix builds both macOS
architectures plus x86_64 Linux and Windows, and uploads uniformly named 7-Zip
packages.

## Runtime configuration

- `DSH_WORKSPACE`: working directory used by DSH; defaults to the user's home.
- `DSH_BUNX_PATH`: absolute path to `bunx` when it cannot be discovered.
- `DSH_PAKE_BINARY`: override the Pake payload path for development/testing.
- `DSH_START_TIMEOUT_SECONDS`: startup timeout; defaults to 300 seconds.
- `DSH_LAUNCHER_LOG`: override the launcher log path.

The default logs are written under `~/Library/Logs/DshDesktop` on macOS,
`$XDG_STATE_HOME/dsh-desktop` (or `~/.local/state/dsh-desktop`) on Linux, and
`%LOCALAPPDATA%\DshDesktop` on Windows.

For an automated local check, run the launcher with `--smoke-test`; it starts
the Pake payload, verifies that it remains alive briefly, and then exits.
