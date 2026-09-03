import path from "node:path";
import process from "node:process";
import metadataDocument from "../APP_METADATA.json" with { type: "json" };
import embeddedVersion from "../VERSION" with { type: "text" };

export type PlatformName = "macos" | "linux" | "windows";
export type Architecture = "x86_64" | "arm64";

export interface ApplicationMetadata {
  displayName: string;
  internalName: string;
  description: string;
  bundleIdentifier: string;
  linuxDesktopId: string;
  dshPackage: string;
  dshURL: string;
  dshPageMarker: string;
}

export interface BuildContext {
  repositoryRoot: string;
  distRoot: string;
  platformOutput: string;
  intermediate: string;
  archivePath: string;
  iconPath: string;
  applicationPackage: string;
  metadata: ApplicationMetadata;
  version: string;
  platform: PlatformName;
  architecture: Architecture;
  smokeTest: boolean;
}

export function createBuildContext(arguments_: string[]): BuildContext {
  const smokeTest = arguments_.includes("--smoke-test");
  const unknownArguments = arguments_.filter((argument) => argument !== "--smoke-test");
  if (unknownArguments.length > 0) {
    throw new Error(`Unknown build arguments: ${unknownArguments.join(", ")}`);
  }

  const metadata = validateMetadata(metadataDocument);
  const version = parseApplicationVersion(embeddedVersion);
  const platform = resolvePlatform();
  const architecture = resolveArchitecture();
  validateNativeTarget(platform, architecture);
  const repositoryRoot = path.resolve(import.meta.dir, "..");
  const distRoot = path.join(repositoryRoot, "dist");
  const platformOutput = path.join(distRoot, platform);
  const intermediate = path.join(distRoot, "intermediate", platform);
  const archiveProductName = metadata.displayName.replaceAll(" ", "-");

  return {
    repositoryRoot,
    distRoot,
    platformOutput,
    intermediate,
    archivePath: path.join(platformOutput, `${archiveProductName}-${platform}-${architecture}.7z`),
    iconPath: path.join(repositoryRoot, "internal", "appicon", "dsh-desktop-icon.png"),
    applicationPackage: "./cmd/dsh-desktop",
    metadata,
    version,
    platform,
    architecture,
    smokeTest,
  };
}

function validateMetadata(value: unknown): ApplicationMetadata {
  if (typeof value !== "object" || value === null) {
    throw new Error("APP_METADATA.json must contain an object");
  }
  const metadata = value as Record<string, unknown>;
  const fields = [
    "displayName",
    "internalName",
    "description",
    "bundleIdentifier",
    "linuxDesktopId",
    "dshPackage",
    "dshURL",
    "dshPageMarker",
  ] as const;
  for (const field of fields) {
    if (typeof metadata[field] !== "string" || metadata[field].trim() === "") {
      throw new Error(`Invalid APP_METADATA.json field: ${field}`);
    }
  }
  return metadata as unknown as ApplicationMetadata;
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

function resolveArchitecture(): Architecture {
  switch (process.arch) {
    case "x64":
      return "x86_64";
    case "arm64":
      return "arm64";
    default:
      throw new Error(`Unsupported architecture: ${process.arch}`);
  }
}

function validateNativeTarget(platform: PlatformName, architecture: Architecture): void {
  if (architecture === "arm64" && platform !== "macos") {
    throw new Error(`${platform}/arm64 is not supported yet`);
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
