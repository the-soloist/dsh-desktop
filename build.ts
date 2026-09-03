import { constants, existsSync } from "node:fs";
import {
  access,
  chmod,
  copyFile,
  mkdir,
  readdir,
  rename,
  rm,
} from "node:fs/promises";
import path from "node:path";

const projectDir = import.meta.dir;
const distDir = path.join(projectDir, "dist");
const platform = process.platform;
const platformName =
  platform === "darwin" ? "macos" : platform === "win32" ? "windows" : "linux";
const architecture = process.arch === "x64" ? "x86_64" : process.arch;
const intermediateDir = path.join(distDir, "intermediate", platformName);
const finalDir = path.join(distDir, platformName);
const archiveName = `DshDesktop-${platformName}-${architecture}.7z`;
const packageOnly = process.argv.includes("--package-only");
const smokeTest = process.argv.includes("--smoke-test");

if (!["darwin", "linux", "win32"].includes(platform)) {
  throw new Error(`Unsupported platform: ${platform}`);
}
if (!["x64", "arm64"].includes(process.arch)) {
  throw new Error(`Unsupported architecture: ${process.arch}`);
}
if (platform !== "darwin" && process.arch !== "x64") {
  throw new Error(`Unsupported target: ${platform}/${process.arch}; only macOS supports arm64`);
}

await mkdir(distDir, { recursive: true });

if (!packageOnly) {
  await resetDirectory(intermediateDir);
  await buildPake();
} else if (!existsSync(intermediateDir)) {
  throw new Error(`Intermediate directory does not exist: ${intermediateDir}`);
}

const launcherBinary = await buildLauncher();
await resetDirectory(finalDir);

let finalApplication: string;
if (platform === "darwin") {
  finalApplication = await packageMacos(launcherBinary);
} else if (platform === "linux") {
  finalApplication = await packageLinux(launcherBinary);
} else {
  finalApplication = await packageWindows(launcherBinary);
}

console.log(`\nBuild complete: ${finalApplication}`);

if (smokeTest) {
  await runSmokeTest(finalApplication);
}

async function buildPake(): Promise<void> {
  const bunx = await findExecutable(
    platform === "win32" ? ["bunx.exe", "bunx.cmd", "bunx.bat"] : ["bunx"],
  );
  if (!bunx) {
    throw new Error("bunx was not found; it is required to run pake-cli");
  }

  const target =
    platform === "darwin"
      ? "app"
      : platform === "linux"
        ? "appimage"
        : "x64";
  const pakeTargetDir = path.join(intermediateDir, "pake-target");

  await run(
    [
      bunx,
      "pake-cli",
      "--config",
      path.join(projectDir, "pake.json"),
      "--targets",
      target,
      "--keep-binary",
    ],
    {
      cwd: intermediateDir,
      env: { CARGO_TARGET_DIR: pakeTargetDir },
    },
  );
}

async function buildLauncher(): Promise<string> {
  const launcherTargetDir = path.join(intermediateDir, "launcher-target");
  await run(["cargo", "build", "--release"], {
    cwd: projectDir,
    env: { CARGO_TARGET_DIR: launcherTargetDir },
  });

  const executableName =
    platform === "win32" ? "dsh-desktop-launcher.exe" : "dsh-desktop-launcher";
  const executable = path.join(launcherTargetDir, "release", executableName);
  await requireFile(executable, "Rust launcher");
  return executable;
}

async function packageMacos(launcherBinary: string): Promise<string> {
  const sourceApp = path.join(intermediateDir, "DshDesktop.app");
  if (!existsSync(sourceApp)) {
    throw new Error(`Pake .app was not found: ${sourceApp}`);
  }

  const stagingDir = path.join(intermediateDir, "package");
  await resetDirectory(stagingDir);
  const stagingApp = path.join(stagingDir, "DshDesktop.app");
  await run(["/usr/bin/ditto", sourceApp, stagingApp]);

  const executableDir = path.join(stagingApp, "Contents", "MacOS");
  const pakeBinary = path.join(executableDir, "pake-dshdesktop");
  const realPakeBinary = path.join(executableDir, "pake-dshdesktop-real");
  await requireFile(pakeBinary, "Pake executable");
  await rename(pakeBinary, realPakeBinary);
  await copyFile(launcherBinary, pakeBinary);
  await chmod(pakeBinary, 0o755);
  await chmod(realPakeBinary, 0o755);

  await run([
    "codesign",
    "--force",
    "--deep",
    "--options",
    "runtime",
    "--sign",
    "-",
    stagingApp,
  ]);
  await run(["codesign", "--verify", "--deep", "--strict", "--verbose=2", stagingApp]);

  const outputApp = path.join(finalDir, "DshDesktop.app");
  await rename(stagingApp, outputApp);

  const sevenZip = await findSevenZip();
  const archive = path.join(finalDir, archiveName);
  await createAndVerifyArchive(sevenZip, archive, [path.basename(outputApp)], finalDir);
  return archive;
}

async function packageLinux(launcherBinary: string): Promise<string> {
  const sourceAppImage = path.join(intermediateDir, "DshDesktop.AppImage");
  await requireFile(sourceAppImage, "Pake AppImage");
  await chmod(sourceAppImage, 0o755);

  const extractDir = path.join(intermediateDir, "extract");
  await resetDirectory(extractDir);
  await run([sourceAppImage, "--appimage-extract"], { cwd: extractDir });

  const appDir = path.join(extractDir, "squashfs-root");
  const binDir = path.join(appDir, "usr", "bin");
  let pakeBinary = path.join(binDir, "pake-dshdesktop");
  if (!existsSync(pakeBinary)) {
    const entries = await readdir(binDir, { withFileTypes: true });
    const candidate = entries.find(
      (entry) => (entry.isFile() || entry.isSymbolicLink()) && entry.name.startsWith("pake-"),
    );
    if (!candidate) {
      throw new Error(`Could not locate the Pake executable under ${binDir}`);
    }
    pakeBinary = path.join(binDir, candidate.name);
  }

  const realPakeBinary = path.join(binDir, "pake-dshdesktop-real");
  await rename(pakeBinary, realPakeBinary);
  await copyFile(launcherBinary, pakeBinary);
  await chmod(pakeBinary, 0o755);
  await chmod(realPakeBinary, 0o755);

  const appImageTool =
    process.env.APPIMAGETOOL ||
    (await findExecutable(["appimagetool"], ["/usr/local/bin", "/usr/bin"]));
  if (!appImageTool) {
    throw new Error("appimagetool was not found; set APPIMAGETOOL to its absolute path");
  }

  const outputAppImage = path.join(finalDir, "DshDesktop.AppImage");
  await run([appImageTool, appDir, outputAppImage], {
    env: { ARCH: "x86_64" },
  });
  await chmod(outputAppImage, 0o755);

  const sevenZip = await findSevenZip();
  const archive = path.join(finalDir, archiveName);
  await createAndVerifyArchive(sevenZip, archive, [path.basename(outputAppImage)], finalDir);
  return archive;
}

async function packageWindows(launcherBinary: string): Promise<string> {
  const sourceBinary = path.join(intermediateDir, "DshDesktop.exe");
  await requireFile(sourceBinary, "Pake executable");

  const packageDir = path.join(intermediateDir, "package");
  await resetDirectory(packageDir);
  await copyFile(launcherBinary, path.join(packageDir, "DshDesktop.exe"));
  await copyFile(sourceBinary, path.join(packageDir, "pake-dshdesktop-real.exe"));

  const sevenZip = await findSevenZip();
  const archive = path.join(finalDir, archiveName);
  await createAndVerifyArchive(
    sevenZip,
    archive,
    ["DshDesktop.exe", "pake-dshdesktop-real.exe"],
    packageDir,
  );
  return archive;
}

async function runSmokeTest(finalApplication: string): Promise<void> {
  const environment = {
    DSH_SMOKE_TEST_SECONDS: "5",
    DSH_LAUNCHER_LOG: path.join(intermediateDir, "smoke-test.log"),
  };

  if (platform === "darwin") {
    await run(
      [path.join(finalDir, "DshDesktop.app", "Contents", "MacOS", "pake-dshdesktop"), "--smoke-test"],
      { env: environment },
    );
  } else if (platform === "linux") {
    await run([path.join(finalDir, "DshDesktop.AppImage"), "--smoke-test"], {
      env: environment,
    });
  } else {
    await run([path.join(intermediateDir, "package", "DshDesktop.exe"), "--smoke-test"], {
      env: environment,
    });
  }
}

async function createAndVerifyArchive(
  sevenZip: string,
  archive: string,
  inputs: string[],
  cwd: string,
): Promise<void> {
  await run([sevenZip, "a", "-t7z", "-mx=9", archive, ...inputs], { cwd });
  await run([sevenZip, "t", archive]);
}

async function findSevenZip(): Promise<string> {
  const extraPaths: string[] = [];
  if (platform === "win32" && process.env.ProgramFiles) {
    extraPaths.push(path.join(process.env.ProgramFiles, "7-Zip"));
  }
  if (platform === "darwin") {
    extraPaths.push("/opt/homebrew/bin", "/usr/local/bin");
  }

  const sevenZip = await findExecutable(["7z", "7zz", "7z.exe"], extraPaths);
  if (!sevenZip) {
    throw new Error("7z was not found; install 7-Zip and ensure it is available on PATH");
  }
  return sevenZip;
}

async function run(
  command: string[],
  options: { cwd?: string; env?: Record<string, string> } = {},
): Promise<void> {
  console.log(`> ${command.map(formatArgument).join(" ")}`);
  const subprocess = Bun.spawn(command, {
    cwd: options.cwd || projectDir,
    env: { ...process.env, ...options.env },
    stdin: "inherit",
    stdout: "inherit",
    stderr: "inherit",
  });
  const exitCode = await subprocess.exited;
  if (exitCode !== 0) {
    throw new Error(`${command[0]} exited with code ${exitCode}`);
  }
}

async function resetDirectory(directory: string): Promise<void> {
  await rm(directory, { recursive: true, force: true });
  await mkdir(directory, { recursive: true });
}

async function requireFile(file: string, description: string): Promise<void> {
  try {
    await access(file, constants.F_OK);
  } catch {
    throw new Error(`${description} was not found: ${file}`);
  }
}

async function findExecutable(names: string[], extraPaths: string[] = []): Promise<string | null> {
  const searchPaths = [...(process.env.PATH || "").split(path.delimiter), ...extraPaths].filter(Boolean);
  for (const directory of searchPaths) {
    for (const name of names) {
      const candidate = path.join(directory, name);
      try {
        await access(candidate, platform === "win32" ? constants.F_OK : constants.X_OK);
        return candidate;
      } catch {
        // Continue searching.
      }
    }
  }
  return null;
}

function formatArgument(argument: string): string {
  return /\s/.test(argument) ? JSON.stringify(argument) : argument;
}
