import { describe, expect, test } from "bun:test";
import { readFileSync } from "node:fs";
import { runInNewContext } from "node:vm";

// Execute the same file embedded in the application. Only the browser/bridge
// boundaries are faked; the interception logic itself is not reimplemented.
const script = readFileSync(new URL("../internal/desktop/download.js", import.meta.url), "utf8");

function browser(bridge = "windows") {
  const location = new URL("http://127.0.0.1:43210/session");
  const messages = [];
  const listeners = [];
  const window = { location };
  const postMessage = (message) => messages.push(JSON.parse(message));
  if (bridge === "windows") window.chrome = { webview: { postMessage } };
  if (bridge === "macos") window.webkit = { messageHandlers: { external: { postMessage } } };
  if (bridge === "linux") window._wails = { invoke: postMessage };
  if (bridge === "broken") window.chrome = { webview: { postMessage() { throw new Error("bridge unavailable"); } } };

  class Anchor {
    constructor(href, download) {
      this.href = new URL(href, location).href;
      this.download = download ?? "";
      this.hasDownload = download !== undefined;
      this.nativeClicks = 0;
    }
    hasAttribute(name) { return name === "download" && this.hasDownload; }
    click() { this.nativeClicks++; }
  }
  const document = {
    addEventListener(name, handler, capture) {
      listeners.push({ name, handler, capture });
    },
  };
  const context = { window, document, HTMLAnchorElement: Anchor, URL };
  const install = () => runInNewContext(script, context);
  install();

  function clickDocument(anchor) {
    const event = {
      target: { closest: () => anchor?.hasDownload ? anchor : null },
      prevented: false,
      stopped: false,
      preventDefault() { this.prevented = true; },
      stopImmediatePropagation() { this.stopped = true; },
    };
    for (const listener of listeners) {
      if (listener.name === "click") listener.handler(event);
      if (event.stopped) break;
    }
    return event;
  }
  return { Anchor, messages, listeners, install, clickDocument };
}

describe("download interceptor", () => {
  for (const bridge of ["windows", "macos", "linux"]) {
    test(`${bridge}: detached download anchor sends exactly one native request`, () => {
      const { Anchor, messages } = browser(bridge);
      const anchor = new Anchor("/api/session.export?id=42", "会话.zip");
      anchor.click();
      expect(anchor.nativeClicks).toBe(0);
      expect(messages).toEqual([{
        type: "dsh-desktop-download",
        url: "http://127.0.0.1:43210/api/session.export?id=42",
        filename: "会话.zip",
      }]);
    });
  }

  test("delegated click prevents browser download after handing off to native", () => {
    const { Anchor, messages, listeners, clickDocument } = browser();
    const event = clickDocument(new Anchor("/export", ""));
    expect(messages).toEqual([{
      type: "dsh-desktop-download", url: "http://127.0.0.1:43210/export", filename: "",
    }]);
    expect(event.prevented).toBe(true);
    expect(event.stopped).toBe(true);
    expect(listeners).toHaveLength(1);
    expect(listeners[0].capture).toBe(true);
  });

  for (const [name, href, download] of [
    ["ordinary link", "/page", undefined],
    ["external origin", "https://example.test/export", "export.zip"],
    ["different local port", "http://127.0.0.1:43211/export", "export.zip"],
  ]) {
    test(`${name}: keeps normal navigation`, () => {
      const { Anchor, messages, clickDocument } = browser();
      const anchor = new Anchor(href, download);
      anchor.click();
      const event = clickDocument(anchor);
      expect(anchor.nativeClicks).toBe(1);
      expect(messages).toHaveLength(0);
      expect(event.prevented).toBe(false);
      expect(event.stopped).toBe(false);
    });
  }

  for (const bridge of ["missing", "broken"]) {
    test(`${bridge} bridge does not swallow a download`, () => {
      const { Anchor, messages, clickDocument } = browser(bridge);
      const anchor = new Anchor("/export", "export.zip");
      anchor.click();
      const event = clickDocument(anchor);
      expect(anchor.nativeClicks).toBe(1);
      expect(messages).toHaveLength(0);
      expect(event.prevented).toBe(false);
    });
  }

  test("repeated installation does not duplicate listeners or requests", () => {
    const { Anchor, messages, listeners, install } = browser();
    install();
    install();
    new Anchor("/export", "export.zip").click();
    expect(messages).toHaveLength(1);
    expect(listeners).toHaveLength(1);
  });

  test("clicks outside a download anchor are untouched", () => {
    const { clickDocument, messages } = browser();
    expect(clickDocument(null).prevented).toBe(false);
    expect(messages).toHaveLength(0);
  });
});
