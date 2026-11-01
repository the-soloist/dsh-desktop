import { describe, expect, test } from "bun:test";
import { lstat, mkdir, mkdtemp, readFile, readdir, rm, symlink, writeFile } from "node:fs/promises";
import path from "node:path";
import { createBuildContext } from "../scripts/build/config";
import { installationPaths, replaceApplication } from "../scripts/build/install";

describe("build arguments", () => {
  test("ordinary builds do not install", () => {
    expect(createBuildContext([])).toMatchObject({ install: false, smokeTest: false });
  });

  for (const flag of ["--install", "install"]) {
    test(`${flag} enables installation alongside smoke testing`, () => {
      expect(createBuildContext([flag, "--smoke-test"])).toMatchObject({ install: true, smokeTest: true });
    });
  }

  test("unknown arguments are rejected", () => {
    expect(() => createBuildContext(["--install", "--invalid"])).toThrow("Unknown build arguments: --invalid");
  });
});

describe("installation paths", () => {
  const context = createBuildContext([]);

  test("macOS uses the packaged app in Applications", () => {
    const paths = installationPaths({ ...context, platform: "macos" }, {}, "/home/example");
    expect(paths).toEqual({
      source: path.join(context.platformOutput, "DSH Desktop.app"),
      target: "/Applications/DSH Desktop.app",
    });
  });

  test("Windows uses the current user's LOCALAPPDATA", () => {
    const paths = installationPaths({ ...context, platform: "windows" }, { LOCALAPPDATA: "D:\\User Data" }, "C:\\Users\\example");
    expect(paths.source).toBe(path.join(context.intermediate, "package", "DSH Desktop.exe"));
    expect(paths.target).toBe("D:\\User Data\\Programs\\DSH Desktop\\DSH Desktop.exe");
  });

  for (const value of [undefined, "", "relative"]) {
    test(`Windows uses its home fallback for LOCALAPPDATA=${value}`, () => {
      const paths = installationPaths({ ...context, platform: "windows" }, { LOCALAPPDATA: value }, "C:\\Users\\example");
      expect(paths.target).toBe("C:\\Users\\example\\AppData\\Local\\Programs\\DSH Desktop\\DSH Desktop.exe");
    });
  }

  test("Linux honors an absolute XDG_DATA_HOME", () => {
    const paths = installationPaths({ ...context, platform: "linux" }, { XDG_DATA_HOME: "/custom/data" }, "/home/example");
    expect(paths.source).toBe(path.join(context.platformOutput, "DSH Desktop.AppImage"));
    expect(paths.target).toBe("/custom/data/dshdesktop/DSH Desktop.AppImage");
  });

  for (const value of [undefined, "", "relative"]) {
    test(`Linux uses its home fallback for XDG_DATA_HOME=${value}`, () => {
      const paths = installationPaths({ ...context, platform: "linux" }, { XDG_DATA_HOME: value }, "/home/example");
      expect(paths.target).toBe("/home/example/.local/share/dshdesktop/DSH Desktop.AppImage");
    });
  }
});

async function withInstallation(check: (root: string) => Promise<void>): Promise<void> {
  const base = path.resolve(import.meta.dir, "../dist/intermediate/install-tests");
  await mkdir(base, { recursive: true });
  const root = await mkdtemp(path.join(base, "case-"));
  try {
    await check(root);
  } finally {
    await rm(root, { recursive: true, force: true });
  }
}

async function appBundle(directory: string, content: string): Promise<void> {
  const executable = path.join(directory, "Contents", "MacOS", "DSH Desktop");
  await mkdir(path.dirname(executable), { recursive: true });
  await writeFile(executable, content, { mode: 0o755 });
}

describe("application installation", () => {
  test("installs an app bundle and keeps the build artifact", async () => {
    await withInstallation(async (root) => {
      const source = path.join(root, "build", "DSH Desktop.app");
      const target = path.join(root, "Applications", "DSH Desktop.app");
      await appBundle(source, "new application");
      let validated = false;
      const backup = await replaceApplication({ source, target }, path.join(root, "backups"), async (staged) => {
        expect(await readFile(path.join(staged, "Contents/MacOS/DSH Desktop"), "utf8")).toBe("new application");
        validated = true;
      });
      expect(validated).toBe(true);
      expect(backup).toBeUndefined();
      expect(await readFile(path.join(target, "Contents/MacOS/DSH Desktop"), "utf8")).toBe("new application");
      expect(await readFile(path.join(source, "Contents/MacOS/DSH Desktop"), "utf8")).toBe("new application");
      expect(await readdir(path.dirname(target))).toEqual(["DSH Desktop.app"]);
      if (process.platform !== "win32") {
        expect((await lstat(path.join(target, "Contents/MacOS/DSH Desktop"))).mode & 0o111).toBe(0o111);
      }
    });
  });

  test("replaces the entire bundle and backs up obsolete files", async () => {
    await withInstallation(async (root) => {
      const source = path.join(root, "build.app");
      const target = path.join(root, "installed.app");
      await appBundle(source, "new");
      await appBundle(target, "old");
      await writeFile(path.join(target, "obsolete"), "old resource");
      const backup = await replaceApplication({ source, target }, path.join(root, "backups"));
      expect(backup).toBeDefined();
      expect(await readFile(path.join(backup!, "Contents/MacOS/DSH Desktop"), "utf8")).toBe("old");
      expect(await readFile(path.join(backup!, "obsolete"), "utf8")).toBe("old resource");
      expect(await readdir(target)).toEqual(["Contents"]);
      expect(await readFile(path.join(target, "Contents/MacOS/DSH Desktop"), "utf8")).toBe("new");
    });
  });

  test("replaces a single executable without disturbing neighboring files", async () => {
    await withInstallation(async (root) => {
      const source = path.join(root, "build.exe");
      const target = path.join(root, "DSH Desktop.exe");
      await writeFile(source, "new", { mode: 0o755 });
      await writeFile(target, "old");
      await writeFile(path.join(root, "unrelated"), "keep");
      const backup = await replaceApplication({ source, target }, path.join(root, "backups"));
      expect(await readFile(target, "utf8")).toBe("new");
      expect(await readFile(backup!, "utf8")).toBe("old");
      expect(await readFile(path.join(root, "unrelated"), "utf8")).toBe("keep");
    });
  });

  test("validation failure preserves the installed application", async () => {
    await withInstallation(async (root) => {
      const source = path.join(root, "build.exe");
      const target = path.join(root, "installed.exe");
      await writeFile(source, "new");
      await writeFile(target, "old");
      await expect(replaceApplication({ source, target }, path.join(root, "backups"), async () => {
        throw new Error("invalid signature");
      })).rejects.toThrow("invalid signature");
      expect(await readFile(target, "utf8")).toBe("old");
      expect((await readdir(root)).some((name) => name.startsWith(".dsh-desktop-install-"))).toBe(false);
    });
  });

  test("a failed publish restores the old application", async () => {
    await withInstallation(async (root) => {
      const source = path.join(root, "build.exe");
      const target = path.join(root, "installed.exe");
      const backups = path.join(root, "backups");
      await writeFile(source, "new");
      await writeFile(target, "old");
      await expect(replaceApplication({ source, target }, backups, async (staged) => {
        // Force the publication rename to fail after the previous file moves.
        await rm(staged);
      })).rejects.toThrow();
      expect(await readFile(target, "utf8")).toBe("old");
      const backupDirectory = (await readdir(backups))[0];
      expect(await readFile(path.join(backups, backupDirectory, "installed.exe"), "utf8")).toBe("old");
      expect((await readdir(root)).some((name) => name.startsWith(".dsh-desktop-install-"))).toBe(false);
    });
  });

  test("backup failure preserves the installed application", async () => {
    await withInstallation(async (root) => {
      const source = path.join(root, "build.exe");
      const target = path.join(root, "installed.exe");
      const backups = path.join(root, "backups");
      await writeFile(source, "new");
      await writeFile(target, "old");
      await writeFile(backups, "not a directory");
      await expect(replaceApplication({ source, target }, backups)).rejects.toThrow();
      expect(await readFile(target, "utf8")).toBe("old");
    });
  });

  test("a missing artifact does not create the installation directory", async () => {
    await withInstallation(async (root) => {
      await expect(replaceApplication({ source: path.join(root, "missing"), target: path.join(root, "install/app") }, path.join(root, "backups"))).rejects.toThrow();
      expect(await readdir(root)).toEqual([]);
    });
  });

  test("a mismatched destination type is preserved", async () => {
    await withInstallation(async (root) => {
      const source = path.join(root, "build.exe");
      const target = path.join(root, "installed.exe");
      await writeFile(source, "new");
      await mkdir(target);
      await writeFile(path.join(target, "keep"), "unrelated data");
      await expect(replaceApplication({ source, target }, path.join(root, "backups"))).rejects.toThrow("unexpected file type");
      expect(await readFile(path.join(target, "keep"), "utf8")).toBe("unrelated data");
    });
  });

  test.skipIf(process.platform === "win32")("preserves bundle symlinks and rejects a symlink destination", async () => {
    await withInstallation(async (root) => {
      const source = path.join(root, "build.app");
      const target = path.join(root, "installed.app");
      await appBundle(source, "new");
      await symlink("MacOS/DSH Desktop", path.join(source, "Contents/current"));
      await replaceApplication({ source, target }, path.join(root, "backups"));
      expect((await lstat(path.join(target, "Contents/current"))).isSymbolicLink()).toBe(true);
      expect(await readFile(path.join(target, "Contents/current"), "utf8")).toBe("new");
      await rm(target, { recursive: true });
      await symlink(source, target);
      await expect(replaceApplication({ source, target }, path.join(root, "backups"))).rejects.toThrow("unexpected file type");
      expect(await readFile(path.join(source, "Contents/MacOS/DSH Desktop"), "utf8")).toBe("new");
    });
  });
});
