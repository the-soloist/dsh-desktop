// Log rendering has its own animation lifecycle; it never controls navigation.
export function createStartupLog(element, jump) {
  const motion = window.matchMedia("(prefers-reduced-motion: reduce)");
  const events = new AbortController();
  let pending = [];
  let renderFrame = 0;
  let scrollFrame = 0;
  let following = true;
  let unread = false;
  let restoring = false;

  const atBottom = () => element.scrollHeight - element.scrollTop - element.clientHeight <= 4;
  const selecting = () => {
    const selection = window.getSelection();
    return selection && !selection.isCollapsed &&
      (element.contains(selection.anchorNode) || element.contains(selection.focusNode));
  };
  function refresh() {
    element.classList.toggle("has-history", element.scrollTop > 4);
    jump.hidden = !unread || following;
  }
  function stopScroll() {
    window.cancelAnimationFrame(scrollFrame);
    scrollFrame = 0;
  }
  function pause() {
    stopScroll();
    following = false;
    refresh();
  }
  function scrollToLatest(animate) {
    stopScroll();
    const from = element.scrollTop;
    const to = Math.max(0, element.scrollHeight - element.clientHeight);
    if (!animate || motion.matches || from === to) {
      element.scrollTop = to;
      refresh();
      return;
    }
    const start = performance.now();
    const tick = (now) => {
      const progress = Math.min(1, (now - start) / 260);
      element.scrollTop = from + (to - from) * (1 - (1 - progress) ** 3);
      refresh();
      scrollFrame = progress < 1 ? window.requestAnimationFrame(tick) : 0;
    };
    scrollFrame = window.requestAnimationFrame(tick);
  }
  function flush() {
    renderFrame = 0;
    if (selecting()) pause();
    const fragment = document.createDocumentFragment();
    const entering = [];
    element.querySelector(".latest")?.classList.remove("latest");
    for (const { step, animate } of pending) {
      const row = document.createElement("div");
      row.className = "log-entry";
      row.classList.toggle("error", step.phase === "failed" || step.phase === "stopped");
      const time = document.createElement("time");
      const date = new Date(step.startedAt);
      time.dateTime = step.startedAt;
      time.textContent = Number.isNaN(date.getTime()) ? "--:--:--" : date.toLocaleTimeString([], {
        hour: "2-digit", minute: "2-digit", second: "2-digit", hour12: false,
      });
      const text = document.createElement(step.code ? "code" : "p");
      text.textContent = step.detail || step.status;
      text.title = step.status;
      const marker = document.createElement("span");
      marker.className = "log-marker";
      marker.setAttribute("aria-hidden", "true");
      row.append(marker, text, time);
      fragment.append(row);
      if (animate) entering.push(row);
    }
    pending = [];
    fragment.lastElementChild?.classList.add("latest");
    element.append(fragment);
    if (following) {
      if (!motion.matches) {
        for (const row of entering) row.animate([
          { opacity: 0, transform: "translateY(10px)" },
          { opacity: row.classList.contains("latest") ? 1 : 0.75, transform: "translateY(0)" },
        ], { duration: 360, easing: "cubic-bezier(0.22, 1, 0.36, 1)" });
      }
      scrollToLatest(!restoring);
    } else if (entering.length) {
      unread = true;
    }
    restoring = false;
    refresh();
  }
  function update(update) {
    if (update.reset) {
      stopScroll();
      window.cancelAnimationFrame(renderFrame);
      renderFrame = 0;
      pending = [];
      element.replaceChildren();
      following = true;
      unread = false;
      restoring = true;
      refresh();
    }
    pending.push(...(update.steps ?? []).map(step => ({ step, animate: !update.reset })));
    if (!renderFrame && pending.length) renderFrame = window.requestAnimationFrame(flush);
  }

  // Interrupt our scroll immediately on user input, even mid-animation.
  element.addEventListener("wheel", pause, { passive: true, signal: events.signal });
  element.addEventListener("touchstart", pause, { passive: true, signal: events.signal });
  element.addEventListener("pointerdown", pause, { signal: events.signal });
  element.addEventListener("keydown", (event) => {
    if (["ArrowUp", "ArrowDown", "PageUp", "PageDown", "Home", "End", " "].includes(event.key)) pause();
  }, { signal: events.signal });
  function resumeAtBottom() {
    if (!scrollFrame) following = atBottom() && !selecting();
    if (following) unread = false;
    refresh();
  }
  element.addEventListener("scroll", resumeAtBottom, { passive: true, signal: events.signal });
  document.addEventListener("pointerup", resumeAtBottom, { signal: events.signal });
  document.addEventListener("selectionchange", () => {
    if (selecting()) pause();
    else resumeAtBottom();
  }, { signal: events.signal });
  jump.addEventListener("click", () => {
    if (selecting()) window.getSelection().removeAllRanges();
    following = true;
    unread = false;
    scrollToLatest(true);
    refresh();
  }, { signal: events.signal });
  motion.addEventListener("change", () => {
    if (!motion.matches) return;
    for (const animation of element.getAnimations({ subtree: true })) animation.cancel();
    if (following) scrollToLatest(false);
  }, { signal: events.signal });

  return {
    update,
    dispose() {
      events.abort();
      stopScroll();
      window.cancelAnimationFrame(renderFrame);
      for (const animation of element.getAnimations({ subtree: true })) animation.cancel();
      pending = [];
    },
  };
}
