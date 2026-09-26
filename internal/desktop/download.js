(function () {
  if (window.__dshDesktopDownloadInterceptor) return;
  window.__dshDesktopDownloadInterceptor = true;

  function send(payload) {
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

  function intercept(anchor) {
    if (!anchor || !anchor.hasAttribute("download")) return false;
    var href = anchor.href;
    if (!href) return false;
    var destination;
    try {
      destination = new URL(href, window.location.href);
    } catch (_) {
      return false;
    }
    if (destination.origin !== window.location.origin) return false;
    var message = JSON.stringify({
      type: "dsh-desktop-download",
      url: destination.href,
      filename: anchor.download || ""
    });
    return send(message);
  }

  // DSH creates the export anchor without appending it to document, so a
  // document click listener alone cannot observe anchor.click().
  var originalAnchorClick = HTMLAnchorElement.prototype.click;
  HTMLAnchorElement.prototype.click = function () {
    if (intercept(this)) return;
    return originalAnchorClick.call(this);
  };

  document.addEventListener("click", function (event) {
    var target = event.target;
    var anchor = target && typeof target.closest === "function" ? target.closest("a[download]") : null;
    if (intercept(anchor)) {
      event.preventDefault();
      event.stopImmediatePropagation();
    }
  }, true);
})();
