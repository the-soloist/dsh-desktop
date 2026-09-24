import { describe, expect, test } from "bun:test";
import { createStartupState, applyStartupUpdate, startupPresentation } from "../internal/desktop/startup/state.js";

const start = Date.parse("2026-09-24T10:00:00Z");
const step = (phase, seconds = 0, rest = {}) => ({
  phase,
  startedAt: new Date(start + seconds * 1000).toISOString(),
  ...rest,
});

describe("startup presentation", () => {
  test("a burst of logs immediately shows the current phase", () => {
    const state = applyStartupUpdate(createStartupState(), {
      reset: true,
      steps: [step("preparing"), step("version", 1), step("launching", 2), step("connecting", 3)],
    });
    expect(startupPresentation(state, start + 3000)).toEqual({
      title: "正在连接 DSH…", message: "", failed: false,
    });
  });

  test("diagnostic output does not change the phase or reset its wait time", () => {
    const state = applyStartupUpdate(createStartupState(), {
      steps: [step("launching"), step("launching", 4), step(undefined, 8, { status: "DSH 依赖已就绪" })],
    });
    expect(state.startedAt).toBe(start);
    expect(startupPresentation(state, start + 9999).message).toBe("");
    expect(startupPresentation(state, start + 10000).message).toBe("已等待 10 秒 · 正在等待服务初始化。");
  });

  test("switching phases restarts the elapsed hint", () => {
    const state = applyStartupUpdate(createStartupState(), {
      steps: [step("launching"), step("connecting", 30)],
    });
    expect(startupPresentation(state, start + 31000).message).toBe("");
  });

  test("failure remains visible despite late output; retry clears it", () => {
    const failed = applyStartupUpdate(createStartupState(), {
      steps: [step("failed", 5, { summary: "网络不可用。", detail: "technical error" }), step("launching", 6)],
    });
    expect(startupPresentation(failed, start + 60000)).toEqual({
      title: "DSH 启动失败", message: "网络不可用。", failed: true,
    });
    const retry = applyStartupUpdate(failed, { reset: true, steps: [step("preparing", 70)] });
    expect(startupPresentation(retry, start + 70000)).toEqual({
      title: "正在准备运行环境…", message: "", failed: false,
    });
  });

  test("reusing a service skips launch and version stages", () => {
    const state = applyStartupUpdate(createStartupState(), { steps: [step("preparing"), step("connecting", 1)] });
    expect(state.phase).toBe("connecting");
  });

  test("stopped service has a recoverable terminal state", () => {
    const state = applyStartupUpdate(createStartupState(), {
      reset: true, steps: [step("stopped", 1, { summary: "服务已停止，请重试。" })],
    });
    expect(startupPresentation(state).failed).toBe(true);
    expect(startupPresentation(state).message).toBe("服务已停止，请重试。");
  });
});
