(function () {
  if (window.__dshDesktopLayout || window.top !== window) return;

  var adapter = window.__dshDesktopLayoutAdapter;
  var pageID = Date.now().toString(36) + "-" + Math.random().toString(36).slice(2);
  var generation = 0;
  var requestID = "";
  var observedFrame = null;
  var owner = null;
  var dragging = null;
  var restored = false;
  var active = true;

  function validWidth(width) {
    return Number.isInteger(width) && width > 0 && width <= 10000;
  }

  function send(operation, side, width) {
    var bridge = window.__dshDesktopBridge;
    return bridge && bridge.send(JSON.stringify({
      type: "dsh-desktop-layout", id: requestID, url: window.location.href,
      operation: operation, side: side, width: width
    }));
  }

  function connect() {
    if (!active || (observedFrame && observedFrame.isConnected)) return;
    owner = null;
    dragging = null;
    observedFrame = adapter.findFrame();
    observer.disconnect();
    if (!observedFrame) {
      observer.observe(document.documentElement, { childList: true, subtree: true });
      return;
    }

    // Watch only ancestor child lists for unmounts. Token streaming and other
    // mutations inside the frame no longer trigger discovery work.
    for (var parent = observedFrame.parentElement; parent; parent = parent.parentElement) {
      observer.observe(parent, { childList: true });
    }
    requestID = pageID + ":" + (++generation);
    restored = false;
    owner = adapter.connect(observedFrame);
    send(owner ? "load" : "unsupported");
  }

  window.__dshDesktopLayout = {
    restore: function (response) {
      if (response.id !== requestID || !owner || restored || dragging) return;
      restored = true;
      var layout = response.layout || {};
      for (var side of ["sidebar", "rightbar"]) {
        var width = layout[side + "Width"];
        if (validWidth(width) && !owner.setWidth(side, width)) {
          send("unsupported");
          return;
        }
      }
    }
  };

  document.addEventListener("pointerdown", function (event) {
    if (event.button !== 0 || !owner) return;
    var handle = owner.dragHandle(event.target);
    if (!handle) return;
    restored = true;
    dragging = { handle: handle, pointer: event.pointerId, owner: owner };
  }, true);

  document.addEventListener("pointerup", function (event) {
    if (!dragging || dragging.pointer !== event.pointerId) return;
    var drag = dragging;
    dragging = null;
    window.requestAnimationFrame(function () {
      if (owner !== drag.owner || !drag.handle.element.isConnected) return;
      var width = owner.readWidth(drag.handle);
      if (validWidth(width)) send("save", drag.handle.side, width);
    });
  }, true);

  document.addEventListener("pointercancel", function () { dragging = null; }, true);
  var observer = new MutationObserver(connect);
  window.addEventListener("pagehide", function () { active = false; observer.disconnect(); });
  window.addEventListener("pageshow", function () { active = true; observedFrame = null; connect(); });
  connect();
})();
