import { copyFile, mkdir } from "node:fs/promises";
import path from "node:path";
import type { BuildContext } from "../config";
import { buildGoBinary, createAndVerifyArchive, goToolEnvironment, runSmokeTest } from "../common";
import { run } from "../command";

export async function buildWindows(context: BuildContext): Promise<void> {
  const packageDirectory = path.join(context.intermediate, "package");
  const sourceDirectory = path.join(context.intermediate, "windows-source");
  const executableName = `${context.metadata.displayName}.exe`;
  const executable = path.join(packageDirectory, executableName);
  const icon = path.join(context.intermediate, `${context.metadata.displayName}.ico`);
  const resourceObject = path.join(sourceDirectory, "rsrc_windows_amd64.syso");

  await mkdir(packageDirectory, { recursive: true });
  await mkdir(sourceDirectory, { recursive: true });
  await copyFile(path.join(context.repositoryRoot, "cmd", "dsh-desktop", "main.go"), path.join(sourceDirectory, "main.go"));
  await run(
    "go",
    [
      "run",
      "./scripts/windows-resources",
      "-input",
      context.iconPath,
      "-ico",
      icon,
      "-syso",
      resourceObject,
      "-arch",
      "amd64",
    ],
    { cwd: context.repositoryRoot, env: goToolEnvironment() },
  );
  await buildGoBinary(context, executable, { CGO_ENABLED: "0" }, "./dist/intermediate/windows/windows-source");
  await createAndVerifyArchive(context.archivePath, packageDirectory, executableName);
  if (context.smokeTest) {
    await runSmokeTest(context, executable);
  }
}
