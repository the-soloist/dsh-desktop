import * as wails from "/wails/runtime.js";

const timeline = document.querySelector(".timeline");
const pendingSteps = [];
const minimumStepInterval = 180;
let lastStepTime = 0;
let timer = 0;
let generation = 0;

function displayTime(value) {
  const date = new Date(value);
  if (Number.isNaN(date.getTime())) return "--:--:--.---";
  const time = date.toLocaleTimeString([], {
    hour: "2-digit",
    minute: "2-digit",
    second: "2-digit",
    hour12: false,
  });
  return `${time}.${String(date.getMilliseconds()).padStart(3, "0")}`;
}

function completeCurrentStep() {
  const current = timeline.querySelector(".step.current");
  if (!current) return;
  current.classList.remove("current");
  current.classList.add("complete");
}

function renderStep(step) {
  completeCurrentStep();

  const item = document.createElement("article");
  item.className = `step ${step.failed ? "failed" : "current"} entering`;

  const rail = document.createElement("span");
  rail.className = "rail";
  rail.setAttribute("aria-hidden", "true");

  const content = document.createElement("div");
  content.className = "content";

  const heading = document.createElement("div");
  heading.className = "step-heading";

  const title = document.createElement("h2");
  title.textContent = step.status;

  const startedAt = document.createElement("time");
  startedAt.dateTime = step.startedAt;
  startedAt.textContent = displayTime(step.startedAt);

  const detail = document.createElement("p");
  detail.textContent = step.detail;

  heading.append(title, startedAt);
  content.append(heading);
  if (step.detail) content.append(detail);
  item.append(rail, content);
  timeline.append(item);

  requestAnimationFrame(() => requestAnimationFrame(() => item.classList.remove("entering")));
  item.scrollIntoView({ behavior: "smooth", block: "nearest" });

  if (step.navigate) {
    window.setTimeout(() => void wails.Events.Emit("startup:navigate"), 320);
  }
}

function drainSteps(expectedGeneration) {
  if (expectedGeneration !== generation || pendingSteps.length === 0) {
    timer = 0;
    return;
  }
  const delay = Math.max(0, minimumStepInterval - (performance.now() - lastStepTime));
  timer = window.setTimeout(() => {
    if (expectedGeneration !== generation) {
      timer = 0;
      return;
    }
    renderStep(pendingSteps.shift());
    lastStepTime = performance.now();
    timer = 0;
    drainSteps(expectedGeneration);
  }, delay);
}

function receiveUpdate(event) {
  const update = event.data ?? event;
  if (update.reset) {
    generation += 1;
    pendingSteps.length = 0;
    timeline.replaceChildren();
    if (timer) window.clearTimeout(timer);
    timer = 0;
    lastStepTime = 0;
  }
  pendingSteps.push(...(update.steps ?? []));
  if (!timer) drainSteps(generation);
}

wails.Events.On("startup:update", receiveUpdate);
void wails.Events.Emit("startup:frontend-ready");
