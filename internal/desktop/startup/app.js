import * as wails from "/wails/runtime.js";
import { createStartupState, applyStartupUpdate, startupPresentation } from "./state.js";

const status = document.querySelector(".status");
const title = document.querySelector("#status-title");
const message = document.querySelector("#status-message");
const details = document.querySelector(".details");
const log = document.querySelector(".log");
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
  actions.hidden = !view.failed && !details.open;
  retry.hidden = !view.failed;
}

function appendLog(step) {
  const row = document.createElement("div");
  row.className = "log-entry";
  const heading = document.createElement("div");
  const time = document.createElement("time");
  const date = new Date(step.startedAt);
  time.dateTime = step.startedAt;
  time.textContent = Number.isNaN(date.getTime()) ? "--:--:--" : date.toLocaleTimeString([], { hour12: false });
  const label = document.createElement("span");
  label.textContent = step.status;
  heading.append(time, label);
  row.append(heading);
  if (step.detail) {
    const detail = document.createElement(step.code ? "code" : "p");
    detail.textContent = step.detail;
    row.append(detail);
  }
  log.append(row);
}

wails.Events.On("startup:update", (event) => {
  const update = event.data ?? event;
  const follow = log.scrollHeight - log.scrollTop - log.clientHeight < 24;
  if (update.reset) {
    log.replaceChildren();
    details.open = false;
    retry.disabled = false;
    copy.disabled = false;
    feedback.textContent = "";
  }
  state = applyStartupUpdate(state, update);
  for (const step of update.steps ?? []) appendLog(step);
  renderStatus();
  if (details.open && follow) log.scrollTop = log.scrollHeight;
});

details.addEventListener("toggle", renderStatus);
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
    feedback.textContent = "复制失败，请展开启动详情手动复制。";
  }
});
wails.Events.On("startup:diagnostics-copied", (event) => {
  copy.disabled = false;
  feedback.textContent = event.data === true ? "诊断信息已复制" : "复制失败，请展开启动详情手动复制。";
});

renderStatus();
const clock = window.setInterval(renderStatus, 1000);
window.addEventListener("pagehide", () => window.clearInterval(clock), { once: true });
void wails.Events.Emit("startup:frontend-ready");
