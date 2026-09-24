function (percent, show) {
  const id = "dsh-desktop-zoom";
  let host = document.getElementById(id);
  if (!host) {
    host = document.createElement("div");
    host.id = id;
    // Isolate the indicator from both the startup and DSH styles. Attaching to
    // <html> also keeps it outside React's root and transformed body containers.
    host.attachShadow({ mode: "open" }).innerHTML = `
      <style>
        span {
          display: block; padding: 5px 9px; border-radius: 7px;
          border: 1px solid rgba(181, 193, 207, .3);
          background: rgba(29, 39, 62, .92); color: #ebe8db;
          box-shadow: 0 2px 8px #0002;
          font: 12px/1.5 -apple-system, BlinkMacSystemFont, "Segoe UI", sans-serif;
          font-variant-numeric: tabular-nums; white-space: nowrap;
          opacity: 0; transform: translateY(4px);
          transition: opacity 180ms ease, transform 180ms ease;
        }
        :host([data-visible]) span { opacity: 1; transform: translateY(0); }
        @media (prefers-reduced-motion: reduce) { span { transition: none; } }
        @media print { span { display: none; } }
      </style>
      <span role="status" aria-live="polite" aria-atomic="true"></span>`;
    document.documentElement.append(host);
    // Pinch zoom can move/resize the visible viewport without changing layout.
    host.positionBadge = () => {
      const viewport = window.visualViewport;
      const scale = host.pageZoom * (viewport?.scale ?? 1);
      const right = document.documentElement.clientWidth - (viewport?.offsetLeft ?? 0) - (viewport?.width ?? innerWidth);
      const bottom = document.documentElement.clientHeight - (viewport?.offsetTop ?? 0) - (viewport?.height ?? innerHeight);
      host.style.cssText = `all: initial !important; position: fixed !important;
        right: ${right + 12 / scale}px !important; bottom: ${bottom + 12 / scale}px !important;
        z-index: 2147483647 !important; pointer-events: none !important;
        transform: scale(${1 / scale}) !important; transform-origin: bottom right !important;`;
    };
    window.visualViewport?.addEventListener("resize", host.positionBadge);
    window.visualViewport?.addEventListener("scroll", host.positionBadge);
  }
  // Keep the badge's physical size/inset steady under both types of zoom.
  host.pageZoom = percent / 100;
  host.positionBadge();
  const label = host.shadowRoot.querySelector("span");
  label.textContent = `${percent}%`;
  label.setAttribute("aria-label", `页面缩放 ${percent}%`);
  if (show) {
    clearTimeout(host.hideTimer);
    host.setAttribute("data-visible", "");
    host.hideTimer = setTimeout(() => host.removeAttribute("data-visible"), 2000);
  }
}
