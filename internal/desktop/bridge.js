(function () {
  if (window.__dshDesktopBridge) return;

  window.__dshDesktopBridge = {
    send: function (payload) {
      try {
        if (window.chrome && window.chrome.webview && window.chrome.webview.postMessage) {
          window.chrome.webview.postMessage(payload);
          return true;
        }
        if (window.webkit && window.webkit.messageHandlers && window.webkit.messageHandlers.external) {
          window.webkit.messageHandlers.external.postMessage(payload);
          return true;
        }
        if (window._wails && typeof window._wails.invoke === "function") {
          window._wails.invoke(payload);
          return true;
        }
      } catch (_) {}
      return false;
    }
  };
})();
