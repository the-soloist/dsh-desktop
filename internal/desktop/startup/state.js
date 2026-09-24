const phases = {
  preparing: { title: "正在准备运行环境…", hint: "正在检查已有实例和运行环境。" },
  version: { title: "正在获取 DSH 版本…", hint: "正在等待版本查询结果。" },
  launching: { title: "正在启动 DSH…", hint: "正在等待服务初始化。" },
  connecting: { title: "正在连接 DSH…", hint: "正在建立本地服务连接。" },
  failed: { title: "DSH 启动失败", failed: true },
  stopped: { title: "DSH 已停止", failed: true },
};

export function createStartupState() {
  return { phase: "preparing", startedAt: 0, summary: "" };
}

// Diagnostic-only records never drive the main status. A failure stays visible
// until a new startup attempt explicitly resets the state.
export function applyStartupUpdate(previous, update) {
  const state = { ...(update.reset ? createStartupState() : previous) };
  for (const step of update.steps ?? []) {
    if (!Object.hasOwn(phases, step.phase) || phases[state.phase].failed) continue;
    if (state.phase !== step.phase || !state.startedAt) {
      state.startedAt = Date.parse(step.startedAt) || 0;
    }
    state.phase = step.phase;
    state.summary = step.summary ?? "";
  }
  return state;
}

export function startupPresentation(state, now = Date.now()) {
  const phase = phases[state.phase];
  const elapsed = state.startedAt ? Math.max(0, Math.floor((now - state.startedAt) / 1000)) : 0;
  let message = "";
  if (phase.failed) {
    message = state.summary || "请查看启动详情，修复问题后重试。";
  } else if (elapsed >= 10) {
    message = `已等待 ${elapsed} 秒 · ${phase.hint}`;
  }
  return { title: phase.title, message, failed: Boolean(phase.failed) };
}
