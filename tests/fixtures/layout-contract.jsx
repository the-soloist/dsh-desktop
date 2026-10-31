import React, { useSyncExternalStore } from "react";
import { createRoot } from "react-dom/client";
import { flushSync } from "react-dom";

// The AppFrame contract from DSH ui-layout: actions live on component props,
// data-shell-overlay is a direct child, and resize handles expose data-side.
// A real production React renderer supplies all host/fiber bookkeeping.
let state, listeners, root;
const viewport = 1400;
const container = document.getElementById("root");
const querySelector = document.querySelector.bind(document);
let discoveries = 0;
document.querySelector = function (selector) {
  if (selector === "[data-shell-overlay]") discoveries++;
  return querySelector(selector);
};

function update(side, width) {
  state = { ...state, [side]: width };
  for (const listener of listeners) listener();
}
const actions = {
  setSidebar(width) { update("sidebar", width); },
  setRightbar(width) { update("rightbar", width); },
};
const subscribe = (listener) => { listeners.add(listener); return () => listeners.delete(listener); };
const snapshot = () => state;

function AppFrame({ actions }) {
  const widths = useSyncExternalStore(subscribe, snapshot);
  return <div id="layout-frame" style={{ width: viewport }}>
    <div data-shell-overlay />
    <div data-stream />
    <div data-side="sidebar" style={{ left: widths.sidebar }}
      onPointerUp={(event) => actions.setSidebar(event.clientX)} />
    <div data-side="rightbar" style={{ left: viewport - widths.rightbar }}
      onPointerUp={(event) => actions.setRightbar?.(viewport - event.clientX)} />
  </div>;
}

function render(compatible = true) {
  if (root) flushSync(() => root.unmount());
  state = { sidebar: 280, rightbar: 500 };
  listeners = new Set();
  root = createRoot(container);
  flushSync(() => root.render(<AppFrame actions={compatible ? actions : { setSidebar: actions.setSidebar }} />));
}

window.layoutFixture = {
  render,
  sample() {
    const frame = document.getElementById("layout-frame");
    return {
      sidebar: parseFloat(frame.querySelector('[data-side="sidebar"]').style.left),
      rightbar: viewport - parseFloat(frame.querySelector('[data-side="rightbar"]').style.left),
      discoveries,
    };
  },
  drag(side, width) {
    const handle = container.querySelector(`[data-side="${side}"]`);
    handle.dispatchEvent(new PointerEvent("pointerdown", { bubbles: true, button: 0, pointerId: 1 }));
    flushSync(() => handle.dispatchEvent(new PointerEvent("pointerup", {
      bubbles: true, button: 0, pointerId: 1, clientX: side === "sidebar" ? width : viewport - width,
    })));
  },
  async stream() {
    const stream = container.querySelector("[data-stream]");
    for (let index = 0; index < 1000; index++) stream.appendChild(document.createTextNode("token "));
    await new Promise((resolve) => requestAnimationFrame(() => requestAnimationFrame(resolve)));
  },
};
render();
window.webkit.messageHandlers.external.postMessage(JSON.stringify({ type: "layout-contract-ready" }));
