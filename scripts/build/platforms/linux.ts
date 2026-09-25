import { chmod, copyFile, mkdir, symlink, writeFile } from "node:fs/promises";
import path from "node:path";
import process from "node:process";
import type { BuildContext } from "../config";
import { buildGoBinary, createAndVerifyArchive, runSmokeTest } from "../common";
import { run } from "../command";
import { linuxDesktopEntry } from "../templates";

export async function buildLinux(context: BuildContext): Promise<void> {
  const appDir = path.join(context.intermediate, `${context.metadata.internalName}.AppDir`);
  const binaryDirectory = path.join(appDir, "usr", "bin");
  const binary = path.join(binaryDirectory, context.metadata.displayName);
  const desktopFile = `${context.metadata.linuxDesktopId}.desktop`;
  const iconFile = `${context.metadata.linuxDesktopId}.png`;
  const appImageName = `${context.metadata.displayName}.AppImage`;
  const appImage = path.join(context.platformOutput, appImageName);

  await mkdir(binaryDirectory, { recursive: true });
  await buildGoBinary(context, binary, { CGO_ENABLED: "1" });
  await chmod(binary, 0o755);
  await symlink(`usr/bin/${context.metadata.displayName}`, path.join(appDir, "AppRun"));
  await writeFile(path.join(appDir, desktopFile), linuxDesktopEntry(context.metadata), "utf8");
  await copyFile(context.iconPath, path.join(appDir, iconFile));

  const applicationsDirectory = path.join(appDir, "usr", "share", "applications");
  const iconsDirectory = path.join(appDir, "usr", "share", "icons", "hicolor", "1024x1024", "apps");
  await mkdir(applicationsDirectory, { recursive: true });
  await mkdir(iconsDirectory, { recursive: true });
  await copyFile(path.join(appDir, desktopFile), path.join(applicationsDirectory, desktopFile));
  await copyFile(path.join(appDir, iconFile), path.join(iconsDirectory, iconFile));

  const appImageTool = process.env.APPIMAGETOOL || Bun.which("appimagetool");
  if (!appImageTool) {
    throw new Error("appimagetool was not found; set APPIMAGETOOL to its absolute path");
  }
  await chmod(appImageTool, 0o755);
  await run(appImageTool, [appDir, appImage], { env: { APPIMAGE_EXTRACT_AND_RUN: "1", ARCH: "x86_64" } });
  await chmod(appImage, 0o755);
  await run("file", [appImage]);
  await createAndVerifyArchive(context.archivePath, context.platformOutput, appImageName);
  if (context.smokeTest) {
    await runSmokeTest(context, appImage, { APPIMAGE_EXTRACT_AND_RUN: "1" });
  }
}
