(function () {
  if (window.__dshDesktopLayoutAdapter) return;

  // This is the only module that knows DSH's DOM and React representation.
  // The protocol/lifecycle module consumes only this small adapter contract.
  function actionsFor(frame) {
    var key = Object.keys(frame).find(function (name) { return name.indexOf("__reactFiber$") === 0; });
    var fiber = key && frame[key];
    for (var depth = 0; fiber && depth < 40; depth++, fiber = fiber.return) {
      var actions = fiber.memoizedProps && fiber.memoizedProps.actions;
      if (actions && typeof actions.setSidebar === "function" && typeof actions.setRightbar === "function") return actions;
    }
    return null;
  }

  window.__dshDesktopLayoutAdapter = {
    findFrame: function () {
      var marker = document.querySelector("[data-shell-overlay]");
      return marker && marker.parentElement;
    },
    connect: function (frame) {
      if (!actionsFor(frame)) return null;
      return {
        frame: frame,
        setWidth: function (side, width) {
          // Re-resolve on restore, so a retained DOM node never captures an old
          // store's actions after a DSH plugin reload.
          var actions = actionsFor(frame);
          if (!actions) return false;
          try {
            actions[side === "sidebar" ? "setSidebar" : "setRightbar"](width);
            return true;
          } catch (_) {
            return false;
          }
        },
        dragHandle: function (target) {
          var element = target && target.closest && target.closest("[data-side]");
          if (!element || element.parentElement !== frame) return null;
          var side = element.getAttribute("data-side");
          return side === "sidebar" || side === "rightbar" ? { element: element, side: side } : null;
        },
        readWidth: function (handle) {
          var left = parseFloat(handle.element.style.left);
          return Math.round(handle.side === "sidebar" ? left : frame.getBoundingClientRect().width - left);
        }
      };
    }
  };
})();
