use std::env;
use std::error::Error;
use std::ffi::{OsStr, OsString};
use std::fs::{self, File, OpenOptions};
use std::io::{self, Write};
use std::net::{IpAddr, Ipv4Addr, SocketAddr, TcpStream};
use std::path::{Path, PathBuf};
use std::process::{Child, Command, ExitStatus, Stdio};
use std::thread;
use std::time::{Duration, Instant, SystemTime, UNIX_EPOCH};

#[cfg(target_os = "windows")]
use std::sync::atomic::{AtomicBool, Ordering};

const DSH_PACKAGE: &str = "@deepseek-ai/dsh@latest";
const DSH_HOST: Ipv4Addr = Ipv4Addr::LOCALHOST;
const DSH_PORT: u16 = 3080;
const DEFAULT_START_TIMEOUT_SECS: u64 = 300;
const DEFAULT_SMOKE_TEST_SECS: u64 = 8;

#[cfg(target_os = "windows")]
static OWNS_STARTUP_CONSOLE: AtomicBool = AtomicBool::new(false);

type AppResult<T> = Result<T, Box<dyn Error>>;

#[derive(Debug)]
struct RuntimeCommand {
    program: PathBuf,
    prefix_args: Vec<OsString>,
    uses_cmd: bool,
    name: &'static str,
}

struct ManagedDsh {
    child: Child,
    active: bool,
}

impl ManagedDsh {
    fn new(child: Child) -> Self {
        Self {
            child,
            active: true,
        }
    }

    fn terminate(&mut self) {
        if !self.active {
            return;
        }

        terminate_process_tree(&mut self.child);
        self.active = false;
    }
}

impl Drop for ManagedDsh {
    fn drop(&mut self) {
        self.terminate();
    }
}

fn main() {
    #[cfg(target_os = "windows")]
    prepare_startup_console();

    let exit_code = match run() {
        Ok(code) => code,
        Err(error) => {
            eprintln!("DshDesktop launcher error: {error}");
            1
        }
    };

    std::process::exit(exit_code);
}

fn run() -> AppResult<i32> {
    let mut log = open_log_file()?;
    log_line(&mut log, "launcher started");

    match run_inner(&mut log) {
        Ok(code) => {
            log_line(&mut log, &format!("launcher stopped with code {code}"));
            Ok(code)
        }
        Err(error) => {
            log_line(&mut log, &format!("ERROR: {error}"));
            Err(error)
        }
    }
}

fn run_inner(log: &mut File) -> AppResult<i32> {
    let workspace = workspace_dir()?;
    let pake_binary = find_pake_binary()?;
    let smoke_test = env::args_os().any(|arg| arg == OsStr::new("--smoke-test"));
    let dsh_home = configured_dsh_home();

    log_line(log, &format!("workspace: {}", workspace.display()));
    log_line(log, &format!("Pake binary: {}", pake_binary.display()));
    if let Some(path) = &dsh_home {
        log_line(log, &format!("DSH_HOME: {}", path.display()));
    } else {
        log_line(
            log,
            "DSH config directory does not exist; DSH_HOME is unset",
        );
    }

    let runtime = match find_bunx() {
        Ok(runtime) => runtime,
        Err(error) => {
            log_line(log, "bunx was not found; showing a warning");
            show_missing_runner_warning();
            return Err(error);
        }
    };
    let child_path = build_child_path(&runtime);
    log_line(
        log,
        &format!(
            "package runner: {} ({})",
            runtime.name,
            runtime.program.display()
        ),
    );

    let address = SocketAddr::new(IpAddr::V4(DSH_HOST), DSH_PORT);
    let mut owned_dsh = if port_is_ready(address) {
        log_line(
            log,
            "127.0.0.1:3080 is already available; reusing the existing server",
        );
        None
    } else {
        log_line(log, &format!("starting DSH with {}", runtime.name));

        let mut managed = ManagedDsh::new(spawn_dsh(
            &runtime,
            &child_path,
            &workspace,
            dsh_home.as_deref(),
            log,
        )?);
        wait_for_dsh(&mut managed.child, address, log)?;
        Some(managed)
    };

    log_line(log, "starting Pake application");
    let mut pake = spawn_pake(
        &pake_binary,
        &child_path,
        &workspace,
        dsh_home.as_deref(),
        log,
    )?;

    #[cfg(target_os = "windows")]
    {
        wait_for_windows_pake_startup(&mut pake, log)?;
        log_line(log, "Pake started successfully; hiding the startup console");
        hide_startup_console();
    }

    let status = if smoke_test {
        let seconds = env_u64("DSH_SMOKE_TEST_SECONDS", DEFAULT_SMOKE_TEST_SECS);
        wait_for_smoke_test(&mut pake, Duration::from_secs(seconds), log)?;
        terminate_child(&mut pake);
        log_line(log, "smoke test passed");
        None
    } else {
        Some(pake.wait()?)
    };

    if let Some(dsh) = owned_dsh.as_mut() {
        log_line(log, "stopping managed DSH server");
        dsh.terminate();
    }

    match status {
        Some(exit_status) => Ok(exit_code(exit_status)),
        None => Ok(0),
    }
}

fn spawn_dsh(
    runtime: &RuntimeCommand,
    child_path: &OsStr,
    workspace: &Path,
    dsh_home: Option<&Path>,
    log: &File,
) -> AppResult<Child> {
    let mut command = if runtime.uses_cmd {
        windows_cmd_command(runtime)
    } else {
        let mut command = Command::new(&runtime.program);
        command.args(&runtime.prefix_args);
        command.arg(DSH_PACKAGE).arg("web").arg("--no-open");
        command
    };

    command
        .current_dir(workspace)
        .env("PATH", child_path)
        .stdin(Stdio::null())
        .stdout(Stdio::from(log.try_clone()?))
        .stderr(Stdio::from(log.try_clone()?));

    if let Some(path) = dsh_home {
        command.env("DSH_HOME", path);
    }

    #[cfg(unix)]
    {
        use std::os::unix::process::CommandExt;
        command.process_group(0);
    }

    Ok(command.spawn()?)
}

#[cfg(target_os = "windows")]
fn windows_cmd_command(runtime: &RuntimeCommand) -> Command {
    let mut invocation = format!("\"{}\"", runtime.program.display());
    for arg in &runtime.prefix_args {
        invocation.push(' ');
        invocation.push_str(&arg.to_string_lossy());
    }
    invocation.push_str(" @deepseek-ai/dsh@latest web --no-open");

    let mut command = Command::new("cmd.exe");
    command.args(["/D", "/S", "/C"]).arg(invocation);
    command
}

#[cfg(not(target_os = "windows"))]
fn windows_cmd_command(_runtime: &RuntimeCommand) -> Command {
    unreachable!("cmd wrappers are only used on Windows")
}

fn spawn_pake(
    pake_binary: &Path,
    child_path: &OsStr,
    workspace: &Path,
    dsh_home: Option<&Path>,
    log: &File,
) -> AppResult<Child> {
    let mut command = Command::new(pake_binary);
    command
        .current_dir(workspace)
        .env("PATH", child_path)
        .stdin(Stdio::null())
        .stdout(Stdio::from(log.try_clone()?))
        .stderr(Stdio::from(log.try_clone()?));
    if let Some(path) = dsh_home {
        command.env("DSH_HOME", path);
    }
    Ok(command.spawn()?)
}

fn wait_for_dsh(child: &mut Child, address: SocketAddr, log: &mut File) -> AppResult<()> {
    let timeout = Duration::from_secs(env_u64(
        "DSH_START_TIMEOUT_SECONDS",
        DEFAULT_START_TIMEOUT_SECS,
    ));
    let deadline = Instant::now() + timeout;

    loop {
        if port_is_ready(address) {
            log_line(log, "DSH is ready at http://127.0.0.1:3080");
            return Ok(());
        }

        if let Some(status) = child.try_wait()? {
            return Err(app_error(format!(
                "DSH exited before port 3080 became ready ({status})"
            )));
        }

        if Instant::now() >= deadline {
            return Err(app_error(format!(
                "DSH did not become ready within {} seconds",
                timeout.as_secs()
            )));
        }

        thread::sleep(Duration::from_millis(250));
    }
}

fn wait_for_smoke_test(child: &mut Child, duration: Duration, log: &mut File) -> AppResult<()> {
    let deadline = Instant::now() + duration;
    while Instant::now() < deadline {
        if let Some(status) = child.try_wait()? {
            return Err(app_error(format!(
                "Pake application exited during smoke test ({status})"
            )));
        }
        thread::sleep(Duration::from_millis(250));
    }

    log_line(
        log,
        &format!("Pake remained running for {} seconds", duration.as_secs()),
    );
    Ok(())
}

#[cfg(target_os = "windows")]
fn wait_for_windows_pake_startup(child: &mut Child, log: &mut File) -> AppResult<()> {
    let deadline = Instant::now() + Duration::from_secs(1);
    while Instant::now() < deadline {
        if let Some(status) = child.try_wait()? {
            return Err(app_error(format!(
                "Pake application exited during startup ({status})"
            )));
        }
        thread::sleep(Duration::from_millis(100));
    }
    log_line(log, "Pake application startup check passed");
    Ok(())
}

fn port_is_ready(address: SocketAddr) -> bool {
    TcpStream::connect_timeout(&address, Duration::from_millis(300)).is_ok()
}

fn find_bunx() -> AppResult<RuntimeCommand> {
    if let Some(explicit) = env::var_os("DSH_BUNX_PATH") {
        let path = PathBuf::from(explicit);
        if is_executable_file(&path) {
            return Ok(runtime_for_path(path));
        }
        return Err(app_error(format!(
            "DSH_BUNX_PATH does not point to a file: {}",
            path.display()
        )));
    }

    let mut bunx_candidates = Vec::new();
    append_path_candidates(&mut bunx_candidates, bunx_names());
    append_common_bun_candidates(&mut bunx_candidates, bunx_names());
    if let Some(path) = first_file(bunx_candidates) {
        return Ok(runtime_for_path(path));
    }

    Err(app_error("bunx was not found. Install Bun and try again"))
}

fn runtime_for_path(path: PathBuf) -> RuntimeCommand {
    let uses_cmd = cfg!(target_os = "windows")
        && matches!(
            path.extension().and_then(OsStr::to_str),
            Some("cmd") | Some("bat")
        );

    RuntimeCommand {
        program: path,
        prefix_args: Vec::new(),
        uses_cmd,
        name: "bunx",
    }
}

fn append_path_candidates(candidates: &mut Vec<PathBuf>, names: &[&str]) {
    if let Some(path) = env::var_os("PATH") {
        for directory in env::split_paths(&path) {
            for name in names {
                candidates.push(directory.join(name));
            }
        }
    }
}

fn append_common_bun_candidates(candidates: &mut Vec<PathBuf>, names: &[&str]) {
    let mut directories = Vec::new();
    if let Some(home) = home_dir() {
        directories.push(home.join(".bun").join("bin"));
    }

    #[cfg(target_os = "macos")]
    {
        directories.push(PathBuf::from("/opt/homebrew/bin"));
        directories.push(PathBuf::from("/usr/local/bin"));
    }

    #[cfg(all(unix, not(target_os = "macos")))]
    {
        directories.push(PathBuf::from("/usr/local/bin"));
        directories.push(PathBuf::from("/usr/bin"));
    }

    #[cfg(target_os = "windows")]
    {
        if let Some(program_files) = env::var_os("ProgramFiles") {
            directories.push(PathBuf::from(program_files).join("nodejs"));
        }
    }

    for directory in directories {
        for name in names {
            candidates.push(directory.join(name));
        }
    }
}

#[cfg(target_os = "windows")]
fn bunx_names() -> &'static [&'static str] {
    &["bunx.exe", "bunx.cmd", "bunx.bat", "bunx"]
}

#[cfg(not(target_os = "windows"))]
fn bunx_names() -> &'static [&'static str] {
    &["bunx"]
}

fn first_file(candidates: Vec<PathBuf>) -> Option<PathBuf> {
    candidates.into_iter().find(|path| is_executable_file(path))
}

#[cfg(unix)]
fn is_executable_file(path: &Path) -> bool {
    use std::os::unix::fs::PermissionsExt;

    path.metadata()
        .map(|metadata| metadata.is_file() && metadata.permissions().mode() & 0o111 != 0)
        .unwrap_or(false)
}

#[cfg(target_os = "windows")]
fn is_executable_file(path: &Path) -> bool {
    path.is_file()
}

fn build_child_path(runtime: &RuntimeCommand) -> OsString {
    let mut paths = Vec::new();
    if let Some(parent) = runtime.program.parent() {
        paths.push(parent.to_path_buf());
    }
    append_runtime_directories(&mut paths);
    if let Some(existing) = env::var_os("PATH") {
        paths.extend(env::split_paths(&existing));
    }

    deduplicate_paths(&mut paths);
    env::join_paths(paths).unwrap_or_else(|_| env::var_os("PATH").unwrap_or_default())
}

fn append_runtime_directories(paths: &mut Vec<PathBuf>) {
    if let Some(home) = home_dir() {
        paths.push(home.join(".bun").join("bin"));
    }

    #[cfg(target_os = "macos")]
    {
        paths.push(PathBuf::from("/opt/homebrew/bin"));
        paths.push(PathBuf::from("/usr/local/bin"));
        paths.push(PathBuf::from("/usr/bin"));
        paths.push(PathBuf::from("/bin"));
    }

    #[cfg(all(unix, not(target_os = "macos")))]
    {
        paths.push(PathBuf::from("/usr/local/bin"));
        paths.push(PathBuf::from("/usr/bin"));
        paths.push(PathBuf::from("/bin"));
    }

    #[cfg(target_os = "windows")]
    {
        if let Some(program_files) = env::var_os("ProgramFiles") {
            paths.push(PathBuf::from(program_files).join("nodejs"));
        }
    }
}

fn deduplicate_paths(paths: &mut Vec<PathBuf>) {
    let mut unique = Vec::new();
    for path in paths.drain(..) {
        if !unique.iter().any(|existing: &PathBuf| existing == &path) {
            unique.push(path);
        }
    }
    *paths = unique;
}

fn find_pake_binary() -> AppResult<PathBuf> {
    if let Some(explicit) = env::var_os("DSH_PAKE_BINARY") {
        let path = PathBuf::from(explicit);
        if path.is_file() {
            return Ok(path);
        }
        return Err(app_error(format!(
            "DSH_PAKE_BINARY does not point to a file: {}",
            path.display()
        )));
    }

    let current_exe = env::current_exe()?;
    let executable_dir = current_exe
        .parent()
        .ok_or_else(|| app_error("launcher executable has no parent directory"))?;

    let mut candidates = vec![executable_dir.join(real_pake_name())];
    if let Some(app_dir) = env::var_os("APPDIR") {
        candidates.push(
            PathBuf::from(app_dir)
                .join("usr")
                .join("bin")
                .join(real_pake_name()),
        );
    }

    first_file(candidates).ok_or_else(|| {
        app_error(format!(
            "Pake payload was not found next to the launcher (expected {})",
            real_pake_name()
        ))
    })
}

#[cfg(target_os = "windows")]
fn real_pake_name() -> &'static str {
    "pake-dshdesktop-real.exe"
}

#[cfg(not(target_os = "windows"))]
fn real_pake_name() -> &'static str {
    "pake-dshdesktop-real"
}

fn workspace_dir() -> AppResult<PathBuf> {
    if let Some(explicit) = env::var_os("DSH_WORKSPACE") {
        let path = PathBuf::from(explicit);
        if path.is_dir() {
            return Ok(path);
        }
        return Err(app_error(format!(
            "DSH_WORKSPACE is not a directory: {}",
            path.display()
        )));
    }

    home_dir()
        .or_else(|| env::current_dir().ok())
        .ok_or_else(|| app_error("could not determine the DSH workspace directory"))
}

fn configured_dsh_home() -> Option<PathBuf> {
    let config_home = env::var_os("XDG_CONFIG_HOME")
        .filter(|value| !value.is_empty())
        .map(PathBuf::from)
        .or_else(|| home_dir().map(|home| home.join(".config")))?;
    let dsh_home = config_home.join("dsh");
    dsh_home.is_dir().then_some(dsh_home)
}

#[cfg(target_os = "macos")]
fn show_missing_runner_warning() {
    let script = concat!(
        "const app = Application.currentApplication(); ",
        "app.includeStandardAdditions = true; ",
        "app.displayDialog(\"未找到 bunx。请先安装 Bun，然后重新启动 DSH Desktop。\", ",
        "{withTitle: \"DSH Desktop\", buttons: [\"好\"], defaultButton: \"好\"});"
    );
    let _ = Command::new("/usr/bin/osascript")
        .arg("-l")
        .arg("JavaScript")
        .arg("-e")
        .arg(script)
        .stdin(Stdio::null())
        .stdout(Stdio::null())
        .stderr(Stdio::null())
        .status();
}

#[cfg(target_os = "windows")]
fn show_missing_runner_warning() {
    use std::os::windows::ffi::OsStrExt;

    const MB_OK: u32 = 0x0000_0000;
    const MB_ICONWARNING: u32 = 0x0000_0030;

    #[link(name = "user32")]
    extern "system" {
        fn MessageBoxW(
            window: *mut std::ffi::c_void,
            text: *const u16,
            caption: *const u16,
            kind: u32,
        ) -> i32;
    }

    let text: Vec<u16> = OsStr::new("未找到 bunx。请先安装 Bun，然后重新启动 DSH Desktop。")
        .encode_wide()
        .chain(Some(0))
        .collect();
    let caption: Vec<u16> = OsStr::new("DSH Desktop")
        .encode_wide()
        .chain(Some(0))
        .collect();

    unsafe {
        MessageBoxW(
            std::ptr::null_mut(),
            text.as_ptr(),
            caption.as_ptr(),
            MB_OK | MB_ICONWARNING,
        );
    }
}

#[cfg(target_os = "windows")]
fn prepare_startup_console() {
    use std::os::windows::ffi::OsStrExt;

    #[link(name = "kernel32")]
    extern "system" {
        fn SetConsoleTitleW(title: *const u16) -> i32;
        fn SetConsoleOutputCP(code_page: u32) -> i32;
        fn GetConsoleProcessList(processes: *mut u32, process_count: u32) -> u32;
    }

    let mut console_processes = [0_u32; 8];
    let title: Vec<u16> = OsStr::new("DSH Desktop - 正在启动")
        .encode_wide()
        .chain(Some(0))
        .collect();
    unsafe {
        let process_count = GetConsoleProcessList(
            console_processes.as_mut_ptr(),
            console_processes.len() as u32,
        );
        OWNS_STARTUP_CONSOLE.store(process_count <= 1, Ordering::Relaxed);
        SetConsoleOutputCP(65001);
        SetConsoleTitleW(title.as_ptr());
    }
}

#[cfg(target_os = "windows")]
fn hide_startup_console() {
    if !OWNS_STARTUP_CONSOLE.load(Ordering::Relaxed) {
        return;
    }

    #[link(name = "kernel32")]
    extern "system" {
        fn GetConsoleWindow() -> *mut std::ffi::c_void;
    }
    #[link(name = "user32")]
    extern "system" {
        fn ShowWindow(window: *mut std::ffi::c_void, command: i32) -> i32;
    }

    const SW_HIDE: i32 = 0;
    unsafe {
        let window = GetConsoleWindow();
        if !window.is_null() {
            ShowWindow(window, SW_HIDE);
        }
    }
}

#[cfg(all(unix, not(target_os = "macos")))]
fn show_missing_runner_warning() {
    const MESSAGE: &str = "未找到 bunx。请先安装 Bun，然后重新启动 DSH Desktop。";

    let helpers = [
        (
            "zenity",
            vec!["--warning", "--title=DSH Desktop", "--text", MESSAGE],
        ),
        (
            "kdialog",
            vec!["--error", MESSAGE, "--title", "DSH Desktop"],
        ),
        ("xmessage", vec!["-center", MESSAGE]),
    ];

    for (name, args) in helpers {
        let mut candidates = Vec::new();
        append_path_candidates(&mut candidates, &[name]);
        candidates.push(PathBuf::from("/usr/bin").join(name));
        candidates.push(PathBuf::from("/usr/local/bin").join(name));
        if let Some(program) = first_file(candidates) {
            let _ = Command::new(program)
                .args(args)
                .stdin(Stdio::null())
                .stdout(Stdio::null())
                .stderr(Stdio::null())
                .status();
            return;
        }
    }
}

fn home_dir() -> Option<PathBuf> {
    #[cfg(target_os = "windows")]
    let value = env::var_os("USERPROFILE");
    #[cfg(not(target_os = "windows"))]
    let value = env::var_os("HOME");

    value.map(PathBuf::from)
}

fn open_log_file() -> AppResult<File> {
    let path = log_path();
    if let Some(parent) = path.parent() {
        fs::create_dir_all(parent)?;
    }

    Ok(OpenOptions::new().create(true).append(true).open(path)?)
}

fn log_path() -> PathBuf {
    if let Some(explicit) = env::var_os("DSH_LAUNCHER_LOG") {
        return PathBuf::from(explicit);
    }

    #[cfg(target_os = "macos")]
    if let Some(home) = home_dir() {
        return home
            .join("Library")
            .join("Logs")
            .join("DshDesktop")
            .join("launcher.log");
    }

    #[cfg(all(unix, not(target_os = "macos")))]
    {
        if let Some(state_home) = env::var_os("XDG_STATE_HOME") {
            return PathBuf::from(state_home)
                .join("dsh-desktop")
                .join("launcher.log");
        }
        if let Some(home) = home_dir() {
            return home
                .join(".local")
                .join("state")
                .join("dsh-desktop")
                .join("launcher.log");
        }
    }

    #[cfg(target_os = "windows")]
    if let Some(local_app_data) = env::var_os("LOCALAPPDATA") {
        return PathBuf::from(local_app_data)
            .join("DshDesktop")
            .join("launcher.log");
    }

    env::temp_dir().join("dsh-desktop-launcher.log")
}

fn log_line(log: &mut File, message: &str) {
    let timestamp = SystemTime::now()
        .duration_since(UNIX_EPOCH)
        .map(|value| value.as_secs())
        .unwrap_or_default();
    let _ = writeln!(log, "[{timestamp}] {message}");
    let _ = log.flush();
    eprintln!("{message}");
}

fn env_u64(name: &str, default: u64) -> u64 {
    env::var(name)
        .ok()
        .and_then(|value| value.parse::<u64>().ok())
        .unwrap_or(default)
}

fn exit_code(status: ExitStatus) -> i32 {
    status.code().unwrap_or(1)
}

fn terminate_child(child: &mut Child) {
    if child.try_wait().ok().flatten().is_none() {
        let _ = child.kill();
    }
    let _ = child.wait();
}

#[cfg(unix)]
fn terminate_process_tree(child: &mut Child) {
    const SIGTERM: i32 = 15;
    const SIGKILL: i32 = 9;

    extern "C" {
        fn kill(pid: i32, signal: i32) -> i32;
    }

    let process_group = -(child.id() as i32);
    unsafe {
        kill(process_group, SIGTERM);
    }

    let deadline = Instant::now() + Duration::from_secs(4);
    while Instant::now() < deadline {
        if child.try_wait().ok().flatten().is_some() {
            break;
        }
        thread::sleep(Duration::from_millis(100));
    }

    unsafe {
        kill(process_group, SIGKILL);
    }
    let _ = child.wait();
}

#[cfg(target_os = "windows")]
fn terminate_process_tree(child: &mut Child) {
    let _ = Command::new("taskkill")
        .args(["/PID", &child.id().to_string(), "/T", "/F"])
        .stdin(Stdio::null())
        .stdout(Stdio::null())
        .stderr(Stdio::null())
        .status();
    terminate_child(child);
}

fn app_error(message: impl Into<String>) -> Box<dyn Error> {
    Box::new(io::Error::other(message.into()))
}
