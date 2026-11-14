// Dashboard reorder. A handle moves a tracker in the viewer's home list, across
// households, or within one personal group. A group handle moves that whole
// group among the ungrouped trackers. The list on the page is updated
// immediately. The server stores the order and answers 204, so the page does
// not reload. Without this script the handles stay hidden and logging still works.
// Dragging a tracker from one group into another is not part of this.

document.documentElement.dataset.reorder = "1";
if (window.trackerReorder) {
  // A swapped dashboard runs this file again. The listeners below stay put.
} else {
  window.trackerReorder = true;

  const dragThreshold = 8;
  let drag = null;
  let saving = false;
  let suppressClick = false;

  document.addEventListener("pointerdown", onPointerDown, { passive: false });
  document.addEventListener("pointermove", onPointerMove, { passive: false });
  document.addEventListener("pointerup", (event) => finishDrag(event, true));
  document.addEventListener("pointercancel", (event) => finishDrag(event, false));
  document.addEventListener(
    "click",
    (event) => {
      if (!suppressClick) return;
      suppressClick = false;
      event.preventDefault();
      event.stopPropagation();
    },
    true,
  );
  document.addEventListener("contextmenu", (event) => {
    if (drag) event.preventDefault();
  });

  function dragTarget(handle) {
    const item = handle.closest("[data-reorder-item]") || handle.closest("[data-group]");
    const list = item && item.parentElement;
    if (!item || !list || !list.dataset.reorderUrl) return null;
    if (list.matches("[data-group-list]")) return { item, list };
    if (list.matches("[data-reorder-list]") && item.parentElement === list) return { item, list };
    return null;
  }

  function isSlot(el, list) {
    if (!el) return false;
    if (list.matches("[data-group-list]")) return el.matches("[data-reorder-item]");
    return el.matches("[data-reorder-item], [data-group]");
  }

  function slotNodes(list) {
    const sel = list.matches("[data-group-list]")
      ? ":scope > [data-reorder-item]"
      : ":scope > [data-reorder-item], :scope > [data-group]";
    return [...list.querySelectorAll(sel)];
  }

  function onPointerDown(event) {
    if (saving || drag || !event.isPrimary || event.button !== 0) return;
    const handle = event.target.closest("[data-drag-handle]");
    const target = handle && dragTarget(handle);
    if (!target) return;
    const item = target.item;
    const list = target.list;
    event.preventDefault();
    // Capture on the document, not the handle. The handle moves with the card, and
    // retargeting it drops the pointer, so the rest of the drag never arrives.
    try {
      document.documentElement.setPointerCapture(event.pointerId);
    } catch {
      // The pointer can already be gone. The move and up handlers no-op without drag.
    }
    drag = {
      pointerId: event.pointerId,
      item,
      list,
      startY: event.clientY,
      y: event.clientY,
      grabOffset: event.clientY - item.getBoundingClientRect().top,
      moved: false,
      previous: slotNodes(list),
      frame: 0,
    };
    document.documentElement.dataset.reordering = "1";
    document.querySelectorAll("[data-refresh]").forEach((el) => {
      if (window.htmx) htmx.trigger(el, "htmx:abort");
    });
  }

  function onPointerMove(event) {
    if (!drag || event.pointerId !== drag.pointerId) return;
    event.preventDefault();
    drag.y = event.clientY;
    if (!drag.moved) {
      if (Math.abs(event.clientY - drag.startY) < dragThreshold) return;
      drag.moved = true;
      drag.item.classList.add("is-dragging");
      drag.frame = requestAnimationFrame(tick);
    }
    // Place immediately. A fast drag can release before the next frame.
    moveDragged(drag);
  }

  function tick() {
    if (!drag || !drag.moved) return;
    const y = drag.y;
    const edge = 72;
    if (y < edge) window.scrollBy(0, -Math.ceil((edge - y) / 8));
    else if (y > window.innerHeight - edge) window.scrollBy(0, Math.ceil((y - (window.innerHeight - edge)) / 8));
    moveDragged(drag);
    drag.frame = requestAnimationFrame(tick);
  }

  // Slide the card with the pointer. Swap when the card's middle passes a neighbor.
  // The handle is at the top of the card, so the card has to move with it.
  function moveDragged(state) {
    const item = state.item;
    item.style.transform = "";
    const height = item.getBoundingClientRect().height;
    const items = slotNodes(state.list);
    let top = Infinity;
    let bottom = -Infinity;
    for (const el of items) {
      const box = el.getBoundingClientRect();
      top = Math.min(top, box.top);
      bottom = Math.max(bottom, box.bottom);
    }
    const desiredTop = Math.min(Math.max(state.y - state.grabOffset, top), bottom - height);
    placeItem(state.list, item, desiredTop + height / 2);
    const laidOut = item.getBoundingClientRect().top;
    item.style.transform = `translateY(${desiredTop - laidOut}px)`;
  }

  function finishDrag(event, commit) {
    if (!drag || event.pointerId !== drag.pointerId) return;
    const state = drag;
    drag = null;
    if (state.frame) cancelAnimationFrame(state.frame);
    state.item.style.transform = "";
    state.item.classList.remove("is-dragging");
    if (!commit || !state.moved) {
      if (state.moved) restoreNodes(state.list, state.previous);
      document.documentElement.removeAttribute("data-reordering");
      return;
    }
    suppressClick = true;
    const current = slotNodes(state.list);
    if (sameNodes(current, state.previous)) {
      document.documentElement.removeAttribute("data-reordering");
      return;
    }
    saveOrder(state.list, state.previous);
  }

  // Swap with the neighbor the card's middle has crossed, and keep going when one
  // move crosses several cards. Stopping at the first neighbor that does not
  // qualify keeps a tall card from jumping back and forth.
  function placeItem(list, item, clientY) {
    const limit = slotNodes(list).length;
    for (let i = 0; i < limit; i++) {
      const next = item.nextElementSibling;
      if (isSlot(next, list)) {
        const box = next.getBoundingClientRect();
        if (clientY > box.top + box.height / 2) {
          next.after(item);
          continue;
        }
      }
      const prev = item.previousElementSibling;
      if (isSlot(prev, list)) {
        const box = prev.getBoundingClientRect();
        if (clientY < box.top + box.height / 2) {
          list.insertBefore(item, prev);
          continue;
        }
      }
      break;
    }
  }

  function sameNodes(a, b) {
    if (a.length !== b.length) return false;
    for (let i = 0; i < a.length; i++) if (a[i] !== b[i]) return false;
    return true;
  }

  function restoreNodes(list, nodes) {
    for (const el of nodes) list.appendChild(el);
  }

  function orderBody(list) {
    const body = new URLSearchParams();
    const nodes = slotNodes(list);
    const grouped = nodes.some((el) => el.matches("[data-group]"));
    if (!grouped) {
      nodes.forEach((el) => body.append("id", el.dataset.id));
      return body;
    }
    nodes.forEach((el) => {
      if (el.matches("[data-group]")) body.append("slot", "g:" + el.dataset.groupId);
      else body.append("slot", "t:" + el.dataset.id);
    });
    return body;
  }

  function saveOrder(list, previous) {
    saving = true;
    const target = new URL(list.dataset.reorderUrl, location.origin);
    const csrf = window.trackCSRF;
    if (!csrf || !csrf.sameOrigin(target.href)) {
      restoreNodes(list, previous);
      saving = false;
      document.documentElement.removeAttribute("data-reordering");
      return;
    }
    const body = orderBody(list);
    fetch(target.href, {
      method: "POST",
      credentials: "same-origin",
      headers: Object.assign({ "Content-Type": "application/x-www-form-urlencoded" }, csrf.headers(target.href)),
      body,
    })
      .then((res) => {
        if (!res.ok) throw new Error(String(res.status));
      })
      .catch(() => {
        restoreNodes(list, previous);
        window.location.reload();
      })
      .finally(() => {
        saving = false;
        document.documentElement.removeAttribute("data-reordering");
      });
  }
}
