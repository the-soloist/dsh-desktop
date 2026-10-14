import { chmod, copyFile, mkdir, writeFile } from "node:fs/promises";
import path from "node:path";
import type { BuildContext } from "../config";
import { buildGoBinary, createAndVerifyArchive, goToolEnvironment, runSmokeTest } from "../common";
import { run } from "../command";
import { macOSInfoPlist } from "../templates";

export async function buildMacOS(context: BuildContext): Promise<void> {
  const binary = path.join(context.intermediate, "dsh-desktop-gui");
  const bundleName = `${context.metadata.displayName}.app`;
  const bundle = path.join(context.platformOutput, bundleName);
  const contents = path.join(bundle, "Contents");
  const macOSDirectory = path.join(contents, "MacOS");
  const resourcesDirectory = path.join(contents, "Resources");
  const packagedBinary = path.join(macOSDirectory, context.metadata.displayName);
  const icon = path.join(resourcesDirectory, `${context.metadata.displayName}.icns`);

  const targets = [
    { goArch: "amd64", nativeArch: "x86_64" },
    { goArch: "arm64", nativeArch: "arm64" },
  ];
  const slices: string[] = [];
  for (const target of targets) {
    const slice = path.join(context.intermediate, `dsh-desktop-gui-${target.nativeArch}`);
    await buildGoBinary(context, slice, {
      GOOS: "darwin",
      GOARCH: target.goArch,
      CC: "clang",
      CGO_ENABLED: "1",
      CGO_CFLAGS: `-arch ${target.nativeArch} -mmacosx-version-min=13.0`,
      CGO_LDFLAGS: `-arch ${target.nativeArch} -mmacosx-version-min=13.0`,
      MACOSX_DEPLOYMENT_TARGET: "13.0",
    });
    slices.push(slice);
  }
  await run("lipo", ["-create", ...slices, "-output", binary]);
  for (const target of targets) {
    await run("lipo", [binary, "-verify_arch", target.nativeArch]);
  }
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
  await run("codesign", ["--verify", "--all-architectures", "--deep", "--strict", "--verbose=2", bundle]);
  await createAndVerifyArchive(context.archivePath, context.platformOutput, bundleName);
  if (context.smokeTest) {
    await runSmokeTest(context, packagedBinary);
  }
}
