import * as wails from "/wails/runtime.js";
import { createStartupState, applyStartupUpdate, startupPresentation } from "./state.js";
import { createStartupLog } from "./log.js";

const status = document.querySelector(".status");
const title = document.querySelector("#status-title");
const message = document.querySelector("#status-message");
const log = createStartupLog(document.querySelector(".log"), document.querySelector("#new-logs"));
const actions = document.querySelector(".actions");
const retry = document.querySelector("#retry");
const copy = document.querySelector("#copy");
const feedback = document.querySelector("#feedback");
let state = createStartupState();

function renderStatus() {
  const view = startupPresentation(state);
  status.classList.toggle("failed", view.failed);
  status.setAttribute("aria-busy", String(!view.failed));
  if (title.textContent !== view.title) title.textContent = view.title;
  message.textContent = view.message;
  message.hidden = !view.message;
  actions.hidden = !view.failed;
  retry.hidden = !view.failed;
}

wails.Events.On("startup:update", (event) => {
  const update = event.data ?? event;
  if (update.reset) {
    retry.disabled = false;
    copy.disabled = false;
    feedback.textContent = "";
  }
  state = applyStartupUpdate(state, update);
  log.update(update);
  renderStatus();
});

retry.addEventListener("click", async () => {
  retry.disabled = true;
  feedback.textContent = "";
  try {
    await wails.Events.Emit("startup:retry");
  } catch {
    retry.disabled = false;
    feedback.textContent = "无法重试，请从托盘菜单重启 DSH。";
  }
});

copy.addEventListener("click", async () => {
  copy.disabled = true;
  feedback.textContent = "";
  try {
    await wails.Events.Emit("startup:copy-diagnostics");
  } catch {
    copy.disabled = false;
    feedback.textContent = "复制失败，请选择日志文字手动复制。";
  }
});
wails.Events.On("startup:diagnostics-copied", (event) => {
  copy.disabled = false;
  feedback.textContent = event.data === true ? "日志已复制" : "复制失败，请选择日志文字手动复制。";
});

renderStatus();
const clock = window.setInterval(renderStatus, 1000);
window.addEventListener("pagehide", () => {
  window.clearInterval(clock);
  log.dispose();
}, { once: true });
void wails.Events.Emit("startup:frontend-ready");
