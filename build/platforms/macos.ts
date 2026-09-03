import { chmod, copyFile, mkdir, writeFile } from "node:fs/promises";
import path from "node:path";
import type { BuildContext } from "../config";
import { buildGoBinary, createAndVerifyArchive, goToolEnvironment, runSmokeTest } from "../common";
import { run } from "../command";
import { macOSInfoPlist } from "../templates";

export async function buildMacOS(context: BuildContext): Promise<void> {
  const binary = path.join(context.intermediate, `${context.metadata.internalName}-binary`);
  const bundleName = `${context.metadata.displayName}.app`;
  const bundle = path.join(context.platformOutput, bundleName);
  const contents = path.join(bundle, "Contents");
  const macOSDirectory = path.join(contents, "MacOS");
  const resourcesDirectory = path.join(contents, "Resources");
  const packagedBinary = path.join(macOSDirectory, context.metadata.displayName);
  const icon = path.join(resourcesDirectory, `${context.metadata.displayName}.icns`);

  await buildGoBinary(context, binary, {
    CGO_ENABLED: "1",
    CGO_CFLAGS: "-mmacosx-version-min=13.0",
    CGO_LDFLAGS: "-mmacosx-version-min=13.0",
    MACOSX_DEPLOYMENT_TARGET: "13.0",
  });
  await mkdir(macOSDirectory, { recursive: true });
  await mkdir(resourcesDirectory, { recursive: true });
  await copyFile(binary, packagedBinary);
  await chmod(packagedBinary, 0o755);
  await run(
    "go",
    ["run", "./scripts/macos-icon", "-input", context.iconPath, "-output", icon],
    { cwd: context.repositoryRoot, env: goToolEnvironment() },
  );
  await writeFile(path.join(contents, "Info.plist"), macOSInfoPlist(context.metadata, context.version), "utf8");
  await run("codesign", ["--force", "--deep", "--sign", "-", bundle]);
  await run("codesign", ["--verify", "--deep", "--strict", "--verbose=2", bundle]);
  await createAndVerifyArchive(context.archivePath, context.platformOutput, bundleName);
  if (context.smokeTest) {
    await runSmokeTest(context, packagedBinary);
  }
}
