import { describe, expect, test } from "bun:test";
import { readFileSync } from "node:fs";
import { runInNewContext } from "node:vm";

const script = ["bridge.js", "clipboard.js"].map((name) =>
  readFileSync(new URL(`../internal/desktop/${name}`, import.meta.url), "utf8")
).join("\n");

function browser({ bridge = "windows", missingClipboard = false, frame = false, denied = false } = {}) {
  const messages = [];
  const timers = new Map();
  const nativeWrites = [];
  let timerID = 0;
  const clipboard = {
    writeText(text) {
      expect(this).toBe(clipboard);
      nativeWrites.push(text);
      if (denied) return Promise.reject(new Error("NotAllowedError"));
      return Promise.resolve();
    },
    readText() { return Promise.resolve("existing clipboard reader"); },
  };
  const navigator = missingClipboard ? {} : { clipboard };
  const window = {
    location: new URL("http://127.0.0.1:43210/session"),
    setTimeout(callback) { timers.set(++timerID, callback); return timerID; },
    clearTimeout(id) { timers.delete(id); },
  };
  window.top = frame ? {} : window;
  const postMessage = (message) => messages.push(JSON.parse(message));
  if (bridge === "windows") window.chrome = { webview: { postMessage } };
  if (bridge === "macos") window.webkit = { messageHandlers: { external: { postMessage } } };
  if (bridge === "linux") window._wails = { invoke: postMessage };
  if (bridge === "broken") window.chrome = { webview: { postMessage() { throw new Error("bridge unavailable"); } } };
  const context = { window, navigator };
  const install = () => runInNewContext(script, context);
  install();
  const complete = (id, ok) => window.__dshDesktopClipboard.complete({ id, ok });
  const expire = () => {
    for (const [id, callback] of [...timers]) { timers.delete(id); callback(); }
  };
  return { window, navigator, messages, timers, nativeWrites, install, complete, expire };
}

// Same success/failure contract as DSH's shared writeClipboard helper.
async function writeClipboard(clipboard, text) {
  try { await clipboard.writeText(text); return true; }
  catch { return false; }
}

describe("desktop clipboard", () => {
  for (const bridge of ["windows", "macos", "linux"]) {
    test(`${bridge}: copy waits for a native success acknowledgement`, async () => {
      const view = browser({ bridge, denied: true });
      const text = "回复内容\n```js\nconst a = '你好 👋';\n```\n";
      let settled = false;
      const copied = writeClipboard(view.navigator.clipboard, text).then((ok) => { settled = true; return ok; });
      await Promise.resolve();
      expect(settled).toBe(false);
      expect(view.messages).toHaveLength(1);
      expect(view.messages[0]).toMatchObject({
        type: "dsh-desktop-clipboard-write", url: view.window.location.href, text,
      });
      expect(view.nativeWrites).toHaveLength(0);
      view.complete(view.messages[0].id, true);
      expect(await copied).toBe(true);
      expect(view.timers.size).toBe(0);
    });
  }

  test("native failure is not presented as a successful copy", async () => {
    const view = browser();
    const copied = writeClipboard(view.navigator.clipboard, "message");
    view.complete(view.messages[0].id, false);
    expect(await copied).toBe(false);
    expect(view.timers.size).toBe(0);
    expect(view.nativeWrites).toHaveLength(0);
  });

  test("a missing response times out, and a late reply is ignored", async () => {
    const view = browser();
    const copied = writeClipboard(view.navigator.clipboard, "message");
    view.expire();
    expect(await copied).toBe(false);
    expect(view.timers.size).toBe(0);
    view.complete(view.messages[0].id, true);
  });

  test("concurrent copies cannot settle each other's promises", async () => {
    const view = browser();
    const first = writeClipboard(view.navigator.clipboard, "first");
    const second = writeClipboard(view.navigator.clipboard, "second");
    expect(view.messages[0].id).not.toBe(view.messages[1].id);
    view.complete(view.messages[1].id, true);
    view.complete(view.messages[0].id, false);
    expect(await first).toBe(false);
    expect(await second).toBe(true);
    expect(view.timers.size).toBe(0);
  });

  test("an absent browser Clipboard API is supplied without adding read access", async () => {
    const view = browser({ missingClipboard: true });
    const copied = view.navigator.clipboard.writeText("");
    expect(view.navigator.clipboard.readText).toBeUndefined();
    expect(view.messages[0].text).toBe("");
    view.complete(view.messages[0].id, true);
    expect(await copied).toBeUndefined();
  });

  for (const bridge of ["missing", "broken"]) {
    test(`${bridge} bridge preserves the original writeText method`, async () => {
      const view = browser({ bridge });
      expect(await writeClipboard(view.navigator.clipboard, "fallback")).toBe(true);
      expect(view.nativeWrites).toEqual(["fallback"]);
      expect(view.messages).toHaveLength(0);
      expect(view.timers.size).toBe(0);
    });
  }

  test("no bridge and no original Clipboard API fails honestly", async () => {
    const view = browser({ bridge: "missing", missingClipboard: true });
    expect(await writeClipboard(view.navigator.clipboard, "fallback")).toBe(false);
    expect(view.timers.size).toBe(0);
  });

  test("readText and repeated installation are unchanged", async () => {
    const view = browser();
    const writeText = view.navigator.clipboard.writeText;
    view.install();
    view.install();
    expect(view.navigator.clipboard.writeText).toBe(writeText);
    expect(await view.navigator.clipboard.readText()).toBe("existing clipboard reader");
    const copied = writeClipboard(view.navigator.clipboard, "once");
    view.complete(view.messages[0].id, true);
    expect(await copied).toBe(true);
    expect(view.messages).toHaveLength(1);
  });

  test("subframes do not receive a native clipboard override", async () => {
    const view = browser({ frame: true });
    expect(view.window.__dshDesktopClipboard).toBeUndefined();
    expect(await writeClipboard(view.navigator.clipboard, "frame")).toBe(true);
    expect(view.messages).toHaveLength(0);
    expect(view.nativeWrites).toEqual(["frame"]);
  });

  test("a previous document's acknowledgement does not settle a new copy", async () => {
    const oldView = browser();
    const oldCopy = writeClipboard(oldView.navigator.clipboard, "old");
    const newView = browser();
    let settled = false;
    const newCopy = writeClipboard(newView.navigator.clipboard, "new").then((ok) => { settled = true; return ok; });
    newView.complete(oldView.messages[0].id, true);
    await Promise.resolve();
    expect(settled).toBe(false);
    newView.complete(newView.messages[0].id, true);
    expect(await newCopy).toBe(true);
    oldView.expire();
    expect(await oldCopy).toBe(false);
  });
});
