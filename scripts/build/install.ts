import { cp, lstat, mkdir, mkdtemp, rename, rm } from "node:fs/promises";
import { homedir } from "node:os";
import path from "node:path";
import process from "node:process";
import { run } from "./command";
import type { BuildContext } from "./config";

interface Installation {
  source: string;
  target: string;
}

export function installationPaths(
  context: BuildContext,
  environment: Record<string, string | undefined> = process.env,
  home = homedir(),
): Installation {
  const { displayName, linuxDesktopId } = context.metadata;
  switch (context.platform) {
    case "macos":
      return {
        source: path.join(context.platformOutput, `${displayName}.app`),
        target: path.posix.join("/Applications", `${displayName}.app`),
      };
    case "windows": {
      const configured = environment.LOCALAPPDATA?.trim() || "";
      const local = path.win32.isAbsolute(configured) ? configured : path.win32.join(home, "AppData", "Local");
      return {
        source: path.join(context.intermediate, "package", `${displayName}.exe`),
        target: path.win32.join(local, "Programs", displayName, `${displayName}.exe`),
      };
    }
    case "linux": {
      const configured = environment.XDG_DATA_HOME?.trim() || "";
      const data = path.posix.isAbsolute(configured) ? configured : path.posix.join(home, ".local", "share");
      return {
        source: path.join(context.platformOutput, `${displayName}.AppImage`),
        target: path.posix.join(data, linuxDesktopId, `${displayName}.AppImage`),
      };
    }
  }
}

export async function installApplication(context: BuildContext): Promise<void> {
  const installation = installationPaths(context);
  if (context.platform === "macos") {
    await run("osascript", ["-e",
      `if application ${JSON.stringify(installation.target)} is running then error "请先完全退出 DSH Desktop，再运行 --install。"`,
    ]);
  }
  const validate = context.platform === "macos"
    ? async (staged: string) => {
      await run("codesign", ["--verify", "--all-architectures", "--deep", "--strict", staged]);
    }
    : undefined;
  const backup = await replaceApplication(installation, path.join(context.distRoot, "install-backups"), validate);
  console.log(`Application installed: ${installation.target}`);
  if (backup) {
    console.log(`Previous application backup: ${backup}`);
  }
}

// Stage beside the destination so publishing and rollback stay on one filesystem.
// Persistent backups live under dist and survive the next platform build.
export async function replaceApplication(
  installation: Installation,
  backupRoot: string,
  validate?: (staged: string) => Promise<void>,
): Promise<string | undefined> {
  const { source, target } = installation;
  const sourceInfo = await lstat(source);
  if (!sourceInfo.isDirectory() && !sourceInfo.isFile()) {
    throw new Error(`Unsupported application artifact: ${source}`);
  }
  const existing = await lstat(target).catch((error: NodeJS.ErrnoException) => {
    if (error.code === "ENOENT") return undefined;
    throw error;
  });
  if (existing && (existing.isSymbolicLink() || existing.isDirectory() !== sourceInfo.isDirectory())) {
    throw new Error(`Installation target has an unexpected file type: ${target}`);
  }

  await mkdir(path.dirname(target), { recursive: true });
  const staging = await mkdtemp(path.join(path.dirname(target), ".dsh-desktop-install-"));
  const staged = path.join(staging, path.basename(target));
  const previous = path.join(staging, "previous");
  const copyOptions = { recursive: true, verbatimSymlinks: true };
  let preserveStaging = false;
  let backup: string | undefined;
  try {
    await cp(source, staged, copyOptions);
    await validate?.(staged);
    if (existing) {
      await mkdir(backupRoot, { recursive: true });
      const directory = await mkdtemp(path.join(backupRoot, "application-"));
      backup = path.join(directory, path.basename(target));
      await cp(target, backup, copyOptions);
      await rename(target, previous);
    }
    try {
      await rename(staged, target);
    } catch (error) {
      if (existing) {
        try {
          await rename(previous, target);
        } catch (restoreError) {
          preserveStaging = true;
          throw new AggregateError([error, restoreError], `Installation rollback failed; previous application: ${previous}; backup: ${backup}`);
        }
      }
      throw error;
    }
  } finally {
    if (!preserveStaging) {
      await rm(staging, { recursive: true, force: true });
    }
  }
  return backup;
}
