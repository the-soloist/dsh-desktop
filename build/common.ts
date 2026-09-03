import { chmod, mkdir, rm } from "node:fs/promises";
import path from "node:path";
import type { BuildContext } from "./config";
import { run } from "./command";

export async function prepareOutput(context: BuildContext): Promise<void> {
  await mkdir(context.distRoot, { recursive: true });
  await resetDirectory(context, context.intermediate);
  await resetDirectory(context, context.platformOutput);
}

export async function buildGoBinary(
  context: BuildContext,
  output: string,
  environment: Record<string, string> = {},
  packageName = context.applicationPackage,
): Promise<void> {
  const linkerFlags = ["-s", "-w"];
  if (context.platform === "windows") {
    linkerFlags.push("-H=windowsgui");
  }
  await run(
    "go",
    [
      "build",
      "-tags",
      "production",
      "-trimpath",
      "-buildvcs=false",
      `-ldflags=${linkerFlags.join(" ")}`,
      "-o",
      output,
      packageName,
    ],
    { cwd: context.repositoryRoot, env: { ...goToolEnvironment(), ...environment } },
  );
}

export function goToolEnvironment(): Record<string, string> {
  return { GOTELEMETRY: "off" };
}

export async function createAndVerifyArchive(
  output: string,
  workingDirectory: string,
  entry: string,
): Promise<void> {
  const sevenZip = Bun.which("7z") || Bun.which("7zz");
  if (!sevenZip) {
    throw new Error("7-Zip was not found (expected 7z or 7zz in PATH)");
  }
  await rm(output, { force: true });
  await run(sevenZip, ["a", "-t7z", "-mx=9", output, entry], { cwd: workingDirectory });
  await run(sevenZip, ["t", output]);
}

export async function runSmokeTest(
  context: BuildContext,
  executable: string,
  environment: Record<string, string> = {},
): Promise<void> {
  console.log("Running startup smoke test");
  // GTK parses application arguments before Wails starts and rejects unknown
  // options, so packaged GUI smoke mode is enabled exclusively via env vars.
  const smokeEnvironment = {
    DSH_SMOKE_TEST: "1",
    DSH_SMOKE_TEST_SECONDS: "5",
    DSH_LAUNCHER_LOG: path.join(context.intermediate, "smoke-launcher.log"),
    ...environment,
  };
  if (context.platform === "linux") {
    const xvfbRun = Bun.which("xvfb-run");
    if (!xvfbRun) {
      throw new Error("xvfb-run was not found; it is required for the Linux smoke test");
    }
    await run(xvfbRun, ["-a", "--", executable], {
      env: {
        ...smokeEnvironment,
        // GitHub-hosted runners do not permit WebKitGTK's bubblewrap process
        // to create a user namespace. Limit this override to the disposable
        // headless smoke test; normal application launches keep the sandbox.
        WEBKIT_DISABLE_SANDBOX_THIS_IS_DANGEROUS: "1",
      },
    });
    return;
  }
  await run(executable, [], { env: smokeEnvironment });
}

async function resetDirectory(context: BuildContext, directory: string): Promise<void> {
  const resolved = path.resolve(directory);
  const resolvedDist = path.resolve(context.distRoot);
  if (resolved === resolvedDist || !resolved.startsWith(`${resolvedDist}${path.sep}`)) {
    throw new Error(`Refusing to reset a directory outside dist: ${resolved}`);
  }
  await rm(resolved, { recursive: true, force: true });
  await mkdir(resolved, { recursive: true });
  await chmod(resolved, 0o755);
}
