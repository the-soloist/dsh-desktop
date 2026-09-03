import {
  chmod,
  copyFile,
  mkdir,
  rm,
  symlink,
  writeFile,
} from "node:fs/promises";
import path from "node:path";
import process from "node:process";
import embeddedVersion from "./VERSION" with { type: "text" };

const repositoryRoot = import.meta.dir;
const distRoot = path.join(repositoryRoot, "dist");
const iconPath = path.join(repositoryRoot, "internal", "appicon", "dsh-desktop-icon.png");
const applicationPackage = "./cmd/dsh-desktop";
const applicationName = "DSH Desktop";
const archiveProductName = applicationName.replaceAll(" ", "-");
const applicationVersion = parseApplicationVersion(embeddedVersion);
const smokeTest = process.argv.slice(2).includes("--smoke-test");
const unknownArguments = process.argv
  .slice(2)
  .filter((argument) => argument !== "--smoke-test");

if (unknownArguments.length > 0) {
  throw new Error(`Unknown build arguments: ${unknownArguments.join(", ")}`);
}

type PlatformName = "macos" | "linux" | "windows";

const platform = resolvePlatform();
const architecture = resolveArchitecture();
validateNativeTarget(platform, architecture);

const platformOutput = path.join(distRoot, platform);
const intermediate = path.join(distRoot, "intermediate", platform);
const archiveName = `${archiveProductName}-${platform}-${architecture}.7z`;
const archivePath = path.join(platformOutput, archiveName);

await mkdir(distRoot, { recursive: true });
await resetDirectory(intermediate);
await resetDirectory(platformOutput);

console.log(`Building ${applicationName} for ${platform}/${architecture}`);

switch (platform) {
  case "macos":
    await buildMacOS();
    break;
  case "linux":
    await buildLinux();
    break;
  case "windows":
    await buildWindows();
    break;
}

console.log(`Package ready: ${path.relative(repositoryRoot, archivePath)}`);

async function buildMacOS(): Promise<void> {
  const binary = path.join(intermediate, "DshDesktop-binary");
  const applicationBundleName = `${applicationName}.app`;
  const applicationBundle = path.join(platformOutput, applicationBundleName);
  const contents = path.join(applicationBundle, "Contents");
  const macOSDirectory = path.join(contents, "MacOS");
  const resourcesDirectory = path.join(contents, "Resources");
  const packagedBinary = path.join(macOSDirectory, applicationName);
  const icnsIcon = path.join(resourcesDirectory, `${applicationName}.icns`);

  await buildGoBinary(binary, {
    CGO_ENABLED: "1",
    CGO_CFLAGS: "-mmacosx-version-min=13.0",
    CGO_LDFLAGS: "-mmacosx-version-min=13.0",
    MACOSX_DEPLOYMENT_TARGET: "13.0",
  });
  await mkdir(macOSDirectory, { recursive: true });
  await mkdir(resourcesDirectory, { recursive: true });
  await copyFile(binary, packagedBinary);
  await chmod(packagedBinary, 0o755);
  await createMacOSIcon(icnsIcon);
  await writeFile(path.join(contents, "Info.plist"), macOSInfoPlist(), "utf8");

  await run("codesign", ["--force", "--deep", "--sign", "-", applicationBundle]);
  await run("codesign", ["--verify", "--deep", "--strict", "--verbose=2", applicationBundle]);
  await createAndVerifyArchive(archivePath, platformOutput, applicationBundleName);

  if (smokeTest) {
    await runSmokeTest(packagedBinary);
  }
}

async function buildLinux(): Promise<void> {
  const appDir = path.join(intermediate, "DshDesktop.AppDir");
  const binaryDirectory = path.join(appDir, "usr", "bin");
  const binary = path.join(binaryDirectory, applicationName);
  const desktopFile = "dshdesktop.desktop";
  const iconFile = "dshdesktop.png";
  const appImageName = `${applicationName}.AppImage`;
  const appImage = path.join(platformOutput, appImageName);

  await mkdir(binaryDirectory, { recursive: true });
  await buildGoBinary(binary, { CGO_ENABLED: "1" });
  await chmod(binary, 0o755);
  await symlink(`usr/bin/${applicationName}`, path.join(appDir, "AppRun"));
  await writeFile(path.join(appDir, desktopFile), linuxDesktopEntry(), "utf8");
  await copyFile(iconPath, path.join(appDir, iconFile));

  const applicationsDirectory = path.join(appDir, "usr", "share", "applications");
  const iconsDirectory = path.join(
    appDir,
    "usr",
    "share",
    "icons",
    "hicolor",
    "1024x1024",
    "apps",
  );
  await mkdir(applicationsDirectory, { recursive: true });
  await mkdir(iconsDirectory, { recursive: true });
  await copyFile(path.join(appDir, desktopFile), path.join(applicationsDirectory, desktopFile));
  await copyFile(path.join(appDir, iconFile), path.join(iconsDirectory, iconFile));

  const appImageTool = process.env.APPIMAGETOOL || Bun.which("appimagetool");
  if (!appImageTool) {
    throw new Error("appimagetool was not found; set APPIMAGETOOL to its absolute path");
  }
  await chmod(appImageTool, 0o755);
  await run(appImageTool, [appDir, appImage], {
    env: { APPIMAGE_EXTRACT_AND_RUN: "1", ARCH: "x86_64" },
  });
  await chmod(appImage, 0o755);
  await run("file", [appImage]);
  await createAndVerifyArchive(archivePath, platformOutput, appImageName);

  if (smokeTest) {
    await runSmokeTest(appImage, { APPIMAGE_EXTRACT_AND_RUN: "1" });
  }
}

async function buildWindows(): Promise<void> {
  const packageDirectory = path.join(intermediate, "package");
  const sourceDirectory = path.join(intermediate, "windows-source");
  const executableName = `${applicationName}.exe`;
  const executable = path.join(packageDirectory, executableName);
  const icoIcon = path.join(intermediate, `${applicationName}.ico`);
  const resourceObject = path.join(sourceDirectory, "rsrc_windows_amd64.syso");

  await mkdir(packageDirectory, { recursive: true });
  await mkdir(sourceDirectory, { recursive: true });
  await copyFile(path.join(repositoryRoot, "cmd", "dsh-desktop", "main.go"), path.join(sourceDirectory, "main.go"));
  await run(
    "go",
    [
      "run",
      "./scripts/windows-resources",
      "-input",
      iconPath,
      "-ico",
      icoIcon,
      "-syso",
      resourceObject,
      "-arch",
      "amd64",
    ],
    { env: goToolEnvironment() },
  );
  await buildGoBinary(executable, { CGO_ENABLED: "0" }, "./dist/intermediate/windows/windows-source");
  await createAndVerifyArchive(archivePath, packageDirectory, executableName);

  if (smokeTest) {
    await runSmokeTest(executable);
  }
}

async function buildGoBinary(
  output: string,
  environment: Record<string, string> = {},
  packageName = applicationPackage,
): Promise<void> {
  await run(
    "go",
    [
      "build",
      "-tags",
      "production",
      "-trimpath",
      "-buildvcs=false",
      "-ldflags=-s -w",
      "-o",
      output,
      packageName,
    ],
    {
      env: { ...goToolEnvironment(), ...environment },
    },
  );
}

function goToolEnvironment(): Record<string, string> {
  return {
    GOCACHE: path.join(intermediate, "go-cache"),
    GOTELEMETRY: "off",
  };
}

async function createMacOSIcon(output: string): Promise<void> {
  await run(
    "go",
    ["run", "./scripts/macos-icon", "-input", iconPath, "-output", output],
    { env: goToolEnvironment() },
  );
}

async function createAndVerifyArchive(
  output: string,
  workingDirectory: string,
  entry: string,
): Promise<void> {
  const sevenZip = Bun.which("7z") || Bun.which("7zz");
  if (!sevenZip) {
    throw new Error("7-Zip was not found (expected 7z or 7zz in PATH)");
  }
  await rm(output, { force: true });
  await run(sevenZip, ["a", "-t7z", "-mx=9", output, entry], {
    cwd: workingDirectory,
  });
  await run(sevenZip, ["t", output]);
}

async function runSmokeTest(
  executable: string,
  environment: Record<string, string> = {},
): Promise<void> {
  console.log("Running startup smoke test");
  await run(executable, ["--smoke-test"], {
    env: { DSH_SMOKE_TEST_SECONDS: "5", ...environment },
  });
}

async function run(
  command: string,
  arguments_: string[],
  options: {
    cwd?: string;
    env?: Record<string, string>;
  } = {},
): Promise<void> {
  console.log(`> ${[command, ...arguments_].map(displayArgument).join(" ")}`);
  const child = Bun.spawn([command, ...arguments_], {
    cwd: options.cwd ?? repositoryRoot,
    env: { ...process.env, ...options.env },
    stdin: "inherit",
    stdout: "inherit",
    stderr: "inherit",
  });
  const exitCode = await child.exited;
  if (exitCode !== 0) {
    throw new Error(`${command} exited with status ${exitCode}`);
  }
}

function displayArgument(value: string): string {
  return /[\s"']/u.test(value) ? JSON.stringify(value) : value;
}

async function resetDirectory(directory: string): Promise<void> {
  const resolved = path.resolve(directory);
  const resolvedDist = path.resolve(distRoot);
  if (resolved === resolvedDist || !resolved.startsWith(`${resolvedDist}${path.sep}`)) {
    throw new Error(`Refusing to reset a directory outside dist: ${resolved}`);
  }
  await rm(resolved, { recursive: true, force: true });
  await mkdir(resolved, { recursive: true });
}

function resolvePlatform(): PlatformName {
  switch (process.platform) {
    case "darwin":
      return "macos";
    case "linux":
      return "linux";
    case "win32":
      return "windows";
    default:
      throw new Error(`Unsupported operating system: ${process.platform}`);
  }
}

function resolveArchitecture(): "x86_64" | "arm64" {
  switch (process.arch) {
    case "x64":
      return "x86_64";
    case "arm64":
      return "arm64";
    default:
      throw new Error(`Unsupported architecture: ${process.arch}`);
  }
}

function validateNativeTarget(targetPlatform: PlatformName, targetArchitecture: string): void {
  if (targetArchitecture === "arm64" && targetPlatform !== "macos") {
    throw new Error(`${targetPlatform}/arm64 is not supported yet`);
  }
}

function parseApplicationVersion(value: string): string {
  const version = value.trim();
  const match = /^(0|[1-9]\d*)\.(0|[1-9]\d*)\.(0|[1-9]\d*)$/u.exec(version);
  if (!match) {
    throw new Error(`Invalid VERSION: ${JSON.stringify(version)}`);
  }
  for (const component of match.slice(1)) {
    if (Number(component) > 65_535) {
      throw new Error(`VERSION component exceeds 65535: ${component}`);
    }
  }
  return version;
}

function macOSInfoPlist(): string {
  return `<?xml version="1.0" encoding="UTF-8"?>
<!DOCTYPE plist PUBLIC "-//Apple//DTD PLIST 1.0//EN" "http://www.apple.com/DTDs/PropertyList-1.0.dtd">
<plist version="1.0">
<dict>
  <key>CFBundleDevelopmentRegion</key>
  <string>zh_CN</string>
  <key>CFBundleDisplayName</key>
  <string>${applicationName}</string>
  <key>CFBundleExecutable</key>
  <string>${applicationName}</string>
  <key>CFBundleIdentifier</key>
  <string>io.github.the-soloist.dsh-desktop</string>
  <key>CFBundleIconFile</key>
  <string>${applicationName}.icns</string>
  <key>CFBundleInfoDictionaryVersion</key>
  <string>6.0</string>
  <key>CFBundleName</key>
  <string>${applicationName}</string>
  <key>CFBundlePackageType</key>
  <string>APPL</string>
  <key>CFBundleShortVersionString</key>
  <string>${applicationVersion}</string>
  <key>CFBundleVersion</key>
  <string>${applicationVersion}</string>
  <key>LSMinimumSystemVersion</key>
  <string>13.0</string>
  <key>NSAppTransportSecurity</key>
  <dict>
    <key>NSAllowsArbitraryLoadsInWebContent</key>
    <true/>
    <key>NSAllowsLocalNetworking</key>
    <true/>
  </dict>
  <key>NSHighResolutionCapable</key>
  <true/>
</dict>
</plist>
`;
}

function linuxDesktopEntry(): string {
  return `[Desktop Entry]
Type=Application
Name=${applicationName}
Comment=Desktop client for DeepSeek DSH
Exec="${applicationName}"
Icon=dshdesktop
Categories=Development;
Terminal=false
`;
}
