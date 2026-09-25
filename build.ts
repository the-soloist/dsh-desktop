import process from "node:process";
import { prepareOutput } from "./scripts/build/common";
import { createBuildContext } from "./scripts/build/config";
import { buildLinux } from "./scripts/build/platforms/linux";
import { buildMacOS } from "./scripts/build/platforms/macos";
import { buildWindows } from "./scripts/build/platforms/windows";

const context = createBuildContext(process.argv.slice(2));
await prepareOutput(context);
console.log(`Building ${context.metadata.displayName} for ${context.platform}/${context.architecture}`);

switch (context.platform) {
  case "macos":
    await buildMacOS(context);
    break;
  case "linux":
    await buildLinux(context);
    break;
  case "windows":
    await buildWindows(context);
    break;
}

console.log(`Package ready: ${context.archivePath}`);
