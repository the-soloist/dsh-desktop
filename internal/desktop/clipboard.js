(function () {
  if (window.__dshDesktopClipboard || window.top !== window) return;

  var clipboard = navigator.clipboard || {};
  var originalWriteText = typeof clipboard.writeText === "function" ? clipboard.writeText.bind(clipboard) : null;
  var pending = new Map();
  // IDs must not collide with an outstanding reply after a reload of the same URL.
  var pageID = Date.now().toString(36) + "-" + Math.random().toString(36).slice(2);
  var sequence = 0;

  function writeText(text) {
    text = String(text);
    return new Promise(function (resolve, reject) {
      var id = pageID + ":" + (++sequence);
      var timeout = window.setTimeout(function () {
        pending.delete(id);
        reject(new Error("Desktop clipboard request timed out"));
      }, 10000);
      pending.set(id, { resolve: resolve, reject: reject, timeout: timeout });
      var bridge = window.__dshDesktopBridge;
      if (bridge && bridge.send(JSON.stringify({
        type: "dsh-desktop-clipboard-write",
        id: id,
        url: window.location.href,
        text: text
      }))) return;

      window.clearTimeout(timeout);
      pending.delete(id);
      if (originalWriteText) {
        try { Promise.resolve(originalWriteText(text)).then(resolve, reject); }
        catch (error) { reject(error); }
      } else {
        reject(new Error("Desktop clipboard bridge is unavailable"));
      }
    });
  }

  try {
    Object.defineProperty(clipboard, "writeText", { configurable: true, writable: true, value: writeText });
    if (!navigator.clipboard) {
      Object.defineProperty(navigator, "clipboard", { configurable: true, value: clipboard });
    }
  } catch (_) {
    return;
  }

  window.__dshDesktopClipboard = {
    complete: function (response) {
      var request = pending.get(response.id);
      if (!request) return;
      pending.delete(response.id);
      window.clearTimeout(request.timeout);
      if (response.ok === true) request.resolve();
      else request.reject(new Error("Desktop clipboard write failed"));
    }
  };
})();
