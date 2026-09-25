import process from "node:process";

export async function run(
  command: string,
  arguments_: string[],
  options: { cwd?: string; env?: Record<string, string> } = {},
): Promise<void> {
  console.log(`> ${[command, ...arguments_].map(displayArgument).join(" ")}`);
  const child = Bun.spawn([command, ...arguments_], {
    cwd: options.cwd,
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
