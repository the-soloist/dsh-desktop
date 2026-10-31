import { describe, expect, test } from "bun:test";
import { readFileSync } from "node:fs";
import { runInNewContext } from "node:vm";

const script = readFileSync(new URL("../internal/desktop/layout.js", import.meta.url), "utf8");

function browser({ sidebar = 280, rightbar = 500, viewport = 1400, compatible = true, mounted = true, restoreFails = false } = {}) {
  const messages = [], calls = [], listeners = new Map(), frames = [];
  const root = {};
  const container = { parentElement: root };
  let frame = { parentElement: container, isConnected: true, getBoundingClientRect: () => ({ width: viewport }) };
  const handles = Object.fromEntries(["sidebar", "rightbar"].map((side) => [side, {
    parentElement: frame, isConnected: true,
    getAttribute: () => side,
    style: { left: `${side === "sidebar" ? sidebar : viewport - rightbar}px` },
    closest() { return this; },
  }]));
  const actions = {
    setSidebar(width) { calls.push(["sidebar", width]); handles.sidebar.style.left = `${width}px`; },
    setRightbar(width) { calls.push(["rightbar", width]); handles.rightbar.style.left = `${viewport - width}px`; },
  };
  let available = mounted;
  let queries = 0;
  const document = {
    documentElement: root,
    addEventListener(name, handler) { listeners.set(name, handler); },
  };
  let observe;
  let disconnected = false;
  const watches = [];
  class MutationObserver {
    constructor(callback) { observe = callback; }
    observe(target, options) { disconnected = false; watches.push({ target, options }); }
    disconnect() { disconnected = true; watches.length = 0; }
  }
  const windowListeners = new Map();
  const window = {
    location: new URL("http://127.0.0.1:41234/session"),
    __dshDesktopBridge: { send(message) { messages.push(JSON.parse(message)); return true; } },
    addEventListener(name, handler) { windowListeners.set(name, handler); },
    requestAnimationFrame(callback) { frames.push(callback); },
    __dshDesktopLayoutAdapter: {
      findFrame() { queries++; return available ? frame : null; },
      connect(candidate) {
        if (!compatible) return null;
        return {
          frame: candidate,
          setWidth(side, width) { if (restoreFails) return false; actions[side === "sidebar" ? "setSidebar" : "setRightbar"](width); return true; },
          dragHandle(element) {
            const entry = Object.entries(handles).find(([, value]) => value === element);
            return entry ? { side: entry[0], element: entry[1] } : null;
          },
          readWidth(handle) { return handle.side === "sidebar" ? parseFloat(handle.element.style.left) : viewport - parseFloat(handle.element.style.left); },
        };
      },
    },
  };
  window.top = window;
  const context = { window, document, MutationObserver };
  const install = () => runInNewContext(script, context);
  install();
  const restore = (layout, id = messages.filter((message) => message.operation === "load").at(-1)?.id) => window.__dshDesktopLayout.restore({ id, layout });
  const pointer = (type, side = "sidebar", id = 1, button = 0) => listeners.get(type)?.({ target: handles[side], pointerId: id, button });
  const flush = () => { for (const callback of frames.splice(0)) callback(); };
  const mount = () => { available = true; observe(); };
  const remount = () => {
    frame.isConnected = false;
    frame = { ...frame, isConnected: true };
    for (const handle of Object.values(handles)) handle.parentElement = frame;
    observe();
  };
  return { window, messages, calls, handles, restore, pointer, flush, mount, remount, watches,
    get frame() { return frame; }, get queries() { return queries; }, observe: () => observe(), install,
    pagehide() { windowListeners.get("pagehide")(); return disconnected; },
    pageshow() { windowListeners.get("pageshow")(); },
  };
}

describe("desktop sidebar widths", () => {
  test("restores both preferences through DSH's existing layout actions", () => {
    const view = browser();
    expect(view.messages).toHaveLength(1);
    expect(view.messages[0]).toMatchObject({ type: "dsh-desktop-layout", operation: "load", url: view.window.location.href });
    view.restore({ sidebarWidth: 350, rightbarWidth: 520 });
    expect(view.calls).toEqual([["sidebar", 350], ["rightbar", 520]]);
    expect(view.messages).toHaveLength(1);
  });

  test("late-mounting DSH layout is restored once", () => {
    const view = browser({ mounted: false });
    expect(view.messages).toHaveLength(0);
    view.mount(); view.observe(); view.install();
    expect(view.messages).toHaveLength(1);
    view.restore({ sidebarWidth: 340 });
    view.restore({ sidebarWidth: 380 });
    expect(view.calls).toEqual([["sidebar", 340]]);
  });

  for (const side of ["sidebar", "rightbar"]) {
    test(`persists ${side} width only after a completed user drag`, () => {
      const view = browser();
      view.restore({});
      view.pointer("pointerdown", side);
      view.pointer("pointerup", side);
      // Simulate React's final onPointerUp delta being committed before rAF.
      view.handles[side].style.left = side === "sidebar" ? "375px" : "800px";
      view.flush();
      expect(view.messages[1]).toMatchObject({ operation: "save", side, width: side === "sidebar" ? 375 : 600 });
    });
  }

  test("responsive resizing and collapse do not overwrite the user's preference", () => {
    const view = browser();
    view.restore({ sidebarWidth: 350 });
    view.handles.sidebar.style.left = "0px";
    view.observe(); view.flush();
    expect(view.messages).toHaveLength(1);
  });

  test("old-page and invalid preferences are ignored", () => {
    const view = browser();
    view.restore({ sidebarWidth: 350 }, "previous-page");
    expect(view.calls).toHaveLength(0);
    view.restore({ sidebarWidth: -1, rightbarWidth: 10001 });
    expect(view.calls).toHaveLength(0);
  });

  test("a slow restore cannot overwrite an active user adjustment", () => {
    const view = browser();
    view.pointer("pointerdown");
    view.restore({ sidebarWidth: 350 });
    expect(view.calls).toHaveLength(0);
    view.pointer("pointerup"); view.flush();
    view.restore({ sidebarWidth: 350 });
    expect(view.calls).toHaveLength(0);
  });

  test("cancelled, unrelated and non-primary gestures are not saved", () => {
    const view = browser();
    view.pointer("pointerdown", "sidebar", 1, 2);
    view.pointer("pointerup");
    view.pointer("pointerdown");
    view.pointer("pointercancel");
    view.pointer("pointerup");
    view.pointer("pointerdown");
    view.pointer("pointerup", "sidebar", 2);
    view.flush();
    expect(view.messages).toHaveLength(1);
  });

  test("unmounted handles and incompatible DSH versions leave the page untouched", () => {
    const view = browser();
    view.pointer("pointerdown"); view.pointer("pointerup");
    view.handles.sidebar.isConnected = false;
    view.flush();
    expect(view.messages).toHaveLength(1);
    const incompatible = browser({ compatible: false });
    incompatible.observe();
    expect(incompatible.messages).toHaveLength(1);
    expect(incompatible.messages[0].operation).toBe("unsupported");
    expect(incompatible.calls).toHaveLength(0);
    expect(view.pagehide()).toBe(true);
  });

  test("connected layout stops document-wide discovery during streaming", () => {
    const view = browser();
    expect(view.watches.every((watch) => !watch.options.subtree)).toBe(true);
    for (let index = 0; index < 1000; index++) view.observe();
    expect(view.queries).toBe(1);
    expect(view.messages).toHaveLength(1);
  });

  test("remount creates a fresh request and rejects the previous owner's reply", () => {
    const view = browser();
    const firstID = view.messages[0].id;
    view.remount();
    expect(view.messages).toHaveLength(2);
    expect(view.messages[1].id).not.toBe(firstID);
    view.restore({ sidebarWidth: 350 }, firstID);
    expect(view.calls).toHaveLength(0);
    view.restore({ sidebarWidth: 370 });
    expect(view.calls).toEqual([["sidebar", 370]]);
  });

  test("a restored browser page reconnects after pagehide", () => {
    const view = browser();
    view.pagehide(); view.observe();
    expect(view.queries).toBe(1);
    view.pageshow();
    expect(view.queries).toBe(2);
    expect(view.messages).toHaveLength(2);
  });

  test("an incompatible setter is reported once and stops restoration", () => {
    const view = browser({ restoreFails: true });
    view.restore({ sidebarWidth: 350, rightbarWidth: 520 });
    expect(view.calls).toHaveLength(0);
    expect(view.messages.filter((message) => message.operation === "unsupported")).toHaveLength(1);
  });
});
