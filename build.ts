import process from "node:process";
import { prepareOutput } from "./build/common";
import { createBuildContext } from "./build/config";
import { buildLinux } from "./build/platforms/linux";
import { buildMacOS } from "./build/platforms/macos";
import { buildWindows } from "./build/platforms/windows";

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
