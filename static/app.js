// Small browser behaviors that HTMX doesn't cover. Keep this file short.

const browserTimeZone = Intl.DateTimeFormat().resolvedOptions().timeZone || "UTC";

// Hidden fields on signup get the browser's IANA time zone.
document.querySelectorAll("input[data-browser-timezone]").forEach((input) => {
  input.value = browserTimeZone;
});

// The magic-link confirm page posts itself. GET still does not use the token,
// so a scanner that only opens the link cannot log in. The button remains for
// browsers without JavaScript.
document.querySelectorAll("form[data-autosubmit]").forEach((form) => {
  const button = form.querySelector("[type=submit]");
  if (button) button.setAttribute("aria-busy", "true");
  form.requestSubmit();
});

// The unit field belongs to number and cumulative trackers. The repeating goal
// belongs to cumulative trackers. Hiding them is optional; the server still checks the kind.
document.querySelectorAll("[data-kind-picker]").forEach((picker) => {
  const sync = () => {
    const unit = picker.querySelector("[data-unit-field]");
    const input = unit && unit.querySelector("input");
    const goal = picker.querySelector("[data-goal-field]");
    const goalInput = goal && goal.querySelector("input");
    const form = picker.closest("form");
    const summary = form && form.querySelector("[data-summary-field]");
    const selected = picker.querySelector('input[name="kind"]:checked');
    const kind = selected ? selected.value : "count";
    const number = kind === "number";
    const cumulative = kind === "cumulative";
    if (unit) unit.hidden = !(number || cumulative);
    if (input) input.disabled = !(number || cumulative);
    if (goal) goal.hidden = !cumulative;
    if (goalInput) goalInput.disabled = !cumulative;
    if (summary) summary.hidden = cumulative;
  };
  picker.addEventListener("change", sync);
  sync();
});

// The log button updates the summary on tap. The server's page replaces it moments later.
// If the request fails, the previous line is put back and an inline note says it was not saved.
document.addEventListener("submit", (event) => {
  const form = event.target.closest("form[data-quick]");
  const tracker = form?.closest("[data-tracker]");
  const summary = tracker?.querySelector("[data-summary]");
  if (!summary) return;
  tracker.querySelector("[data-save-error]")?.remove();
  if (!summary.dataset.optimistic) {
    summary.dataset.prevText = summary.textContent;
    summary.dataset.prevCount = summary.dataset.count ?? "";
    summary.dataset.optimistic = "1";
  }
  const mode = summary.dataset.summaryDisplay || "times";
  if (mode === "done") {
    summary.textContent = "Done today";
    return;
  }
  if (mode === "last") {
    summary.textContent = "Last: " + clockInZone(new Date(), summary.dataset.timeZone);
    return;
  }
  const count = Number(summary.dataset.count) + 1;
  summary.dataset.count = count;
  const times = count === 1 ? "1 time today" : `${count} times today`;
  summary.textContent = `${times} - Last: ${clockInZone(new Date(), summary.dataset.timeZone)}`;
});

function restoreUnsavedLog(event) {
  const el = event.detail?.elt || event.target;
  const form = el?.closest?.("form[data-quick]");
  const tracker = form?.closest("[data-tracker]");
  const summary = tracker?.querySelector("[data-summary]");
  if (!summary?.dataset.optimistic) return;
  summary.textContent = summary.dataset.prevText;
  summary.dataset.count = summary.dataset.prevCount;
  delete summary.dataset.optimistic;
  delete summary.dataset.prevText;
  delete summary.dataset.prevCount;
  let note = tracker.querySelector("[data-save-error]");
  if (!note) {
    note = document.createElement("p");
    note.className = "error";
    note.setAttribute("role", "alert");
    note.dataset.saveError = "";
    summary.after(note);
  }
  note.textContent = "That wasn't saved. Check your connection and try again.";
}

document.addEventListener("htmx:responseError", restoreUnsavedLog);
document.addEventListener("htmx:sendError", restoreUnsavedLog);

// Log and undo disable the clicked button and swap its label for the loader icon.
// The icon uses currentColor, so it follows that button's text color.
function occurrenceButton(elt) {
  if (!elt || typeof elt.querySelector !== "function") return null;
  if (elt.matches?.("button[data-occurrence]")) return elt;
  return elt.querySelector("button[data-occurrence]");
}

function loaderIcon() {
  const icon = document.getElementById("button-spinner")?.content?.querySelector(".icon");
  return icon ? icon.cloneNode(true) : null;
}

function holdOccurrenceButton(button) {
  if (!button || button.dataset.held === "1") return;
  const icon = loaderIcon();
  if (!icon) return;
  const width = button.offsetWidth;
  const height = button.offsetHeight;
  button.dataset.held = "1";
  button.dataset.label = button.innerHTML;
  if (!button.hasAttribute("aria-label")) {
    button.dataset.named = "1";
    button.setAttribute("aria-label", button.textContent.replace(/\s+/g, " ").trim());
  }
  button.style.minWidth = width + "px";
  button.style.minHeight = height + "px";
  button.replaceChildren(icon);
  button.disabled = true;
  button.setAttribute("aria-busy", "true");
}

function releaseOccurrenceButton(button) {
  if (!button || button.dataset.held !== "1" || !button.isConnected) return;
  button.innerHTML = button.dataset.label || "";
  if (button.dataset.named) button.removeAttribute("aria-label");
  button.disabled = false;
  button.removeAttribute("aria-busy");
  button.style.minWidth = "";
  button.style.minHeight = "";
  delete button.dataset.held;
  delete button.dataset.label;
  delete button.dataset.named;
}

document.addEventListener("htmx:beforeRequest", (event) => {
  holdOccurrenceButton(occurrenceButton(event.detail?.elt));
});

document.addEventListener("htmx:afterRequest", (event) => {
  releaseOccurrenceButton(occurrenceButton(event.detail?.elt));
});

// Tracker pages refresh themselves (the autoRefresh partial). Skip it while the tab is
// hidden, a form field has focus, a details section is open, or a request is in flight.
window.trackerIdle = function () {
  if (document.documentElement.dataset.reordering === "1") return false;
  if (document.visibilityState !== "visible") return false;
  const content = document.getElementById("content");
  if (!content || content.querySelector("details[open], .htmx-request")) return false;
  const active = document.activeElement;
  return !(active && content.contains(active) && active.matches("input, textarea, select"));
};

// The poller's own request still has .htmx-request when its response arrives.
// That must not throw the response away. A field focused or a section opened
// after the request started still does.
function refreshStillSafe() {
  if (document.documentElement.dataset.reordering === "1") return false;
  if (document.visibilityState !== "visible") return false;
  const content = document.getElementById("content");
  if (!content || content.querySelector("details[open]")) return false;
  if (content.querySelector(".htmx-request:not([data-refresh])")) return false;
  const active = document.activeElement;
  return !(active && content.contains(active) && active.matches("input, textarea, select"));
}

// View revs tell the server which trackers the page already shows.
window.trackerRefreshVals = function () {
  const ids = [];
  const revs = [];
  document.querySelectorAll("[data-view-rev]").forEach((el) => {
    if (!el.id) return;
    ids.push(el.id);
    revs.push(el.dataset.viewRev || "");
  });
  return { present: ids.join(","), rev: revs.join(",") };
};

// In-place tracker updates. A newer action or a focused field drops a refresh
// that was already on the way. Scroll correction runs only for these updates.
const trackerRequests = new WeakMap();
let trackerEpoch = 0;
const actionEpoch = new Map();
const openTrackerRequests = new Set();
document.addEventListener("scroll", () => {
  for (const info of openTrackerRequests) {
    if (info.closed) continue;
    if (Math.abs(window.scrollY - info.scrollY) > 2) info.moved = true;
  }
}, { passive: true, capture: true });

document.addEventListener("htmx:afterRequest", (event) => {
  const info = trackerRequests.get(event.detail.xhr);
  if (!info || event.detail.successful) return;
  info.closed = true;
  info.skip = true;
  openTrackerRequests.delete(info);
});

document.addEventListener("htmx:beforeRequest", (event) => {
  const elt = event.detail.elt;
  if (!elt || !elt.matches) return;
  const info = { scrollY: window.scrollY, moved: false, closed: false };
  if (elt.matches("[data-refresh]")) {
    info.kind = "refresh";
    info.epoch = trackerEpoch;
    trackerRequests.set(event.detail.xhr, info);
    openTrackerRequests.add(info);
    return;
  }
  const inplace = elt.matches("[data-inplace]") ? elt : elt.closest?.("[data-inplace]");
  trackerEpoch++;
  document.querySelectorAll("[data-refresh]").forEach((el) => htmx.trigger(el, "htmx:abort"));
  if (!inplace) return;
  const root = elt.closest("[data-tracker-id]");
  const id = root ? root.dataset.trackerId : "";
  const n = (actionEpoch.get(id) || 0) + 1;
  actionEpoch.set(id, n);
  info.kind = "action";
  info.id = id;
  info.epoch = n;
  trackerRequests.set(event.detail.xhr, info);
  openTrackerRequests.add(info);
});

document.addEventListener("htmx:beforeSwap", (event) => {
  const info = trackerRequests.get(event.detail.xhr);
  if (!info) return;
  info.closed = true;
  openTrackerRequests.delete(info);
  if (info.kind === "refresh" && (info.epoch !== trackerEpoch || !refreshStillSafe())) {
    event.detail.shouldSwap = false;
    info.skip = true;
    return;
  }
  if (info.kind === "action" && actionEpoch.get(info.id) !== info.epoch) {
    event.detail.shouldSwap = false;
    info.skip = true;
    return;
  }
  info.revs = snapshotTrackerRevs();
  info.anchor = captureScrollAnchor();
});

document.addEventListener("htmx:afterSwap", (event) => {
  const info = trackerRequests.get(event.detail.xhr);
  if (!info || info.skip) return;
  if (info.kind === "refresh") applyTrackerOrder();
  restoreScrollAnchor(info);
});

// HTMX settles class about 20ms after the swap and would wipe a class added earlier.
document.addEventListener("htmx:afterSettle", (event) => {
  const info = trackerRequests.get(event.detail.xhr);
  if (!info || info.skip || info.highlighted) return;
  info.highlighted = true;
  highlightChangedTrackers(info.revs);
});

function snapshotTrackerRevs() {
  const map = new Map();
  document.querySelectorAll("[data-tracker-rev]").forEach((el) => {
    if (el.id) map.set(el.id, el.dataset.trackerRev || "");
  });
  return map;
}

function captureScrollAnchor() {
  const nodes = document.querySelectorAll("[data-scroll-anchor]");
  let above = null;
  for (const el of nodes) {
    if (!el.id) continue;
    if (el.getBoundingClientRect().top <= 1) above = el;
    else return packAnchor(above || el);
  }
  return above ? packAnchor(above) : null;
}

function packAnchor(el) {
  const next = nextAnchor(el, 1);
  const prev = nextAnchor(el, -1);
  return {
    id: el.id,
    top: el.getBoundingClientRect().top,
    next: next ? next.id : "",
    prev: prev ? prev.id : "",
  };
}

function nextAnchor(el, dir) {
  const nodes = [...document.querySelectorAll("[data-scroll-anchor]")].filter((node) => node.id);
  const i = nodes.indexOf(el);
  if (i < 0) return null;
  return nodes[i + dir] || null;
}

function restoreScrollAnchor(info) {
  const saved = info.anchor;
  if (!saved || info.moved) return;
  let el = document.getElementById(saved.id);
  if (!el && saved.next) el = document.getElementById(saved.next);
  if (!el && saved.prev) el = document.getElementById(saved.prev);
  if (!el) return;
  const delta = el.getBoundingClientRect().top - saved.top;
  if (Math.abs(delta) < 1) return;
  window.scrollBy(0, delta);
}

function applyTrackerOrder() {
  const order = document.getElementById("tracker-order");
  const list = document.getElementById("tracker-list");
  if (!order || !list || !order.dataset.order) return;
  for (const id of order.dataset.order.split(",")) {
    if (!id) continue;
    const el = document.getElementById("tracker-" + id);
    if (el && el.parentElement === list) list.appendChild(el);
  }
}

const highlightTimers = new WeakMap();

function highlightChangedTrackers(before) {
  if (!before) return;
  const dirty = new Set();
  document.querySelectorAll("[data-tracker-rev]").forEach((el) => {
    if (!el.id || before.get(el.id) === el.dataset.trackerRev) return;
    const root = el.closest("[data-tracker-id]");
    const id = root && root.dataset.trackerId;
    if (id) dirty.add(id);
  });
  dirty.forEach((id) => {
    const el = document.querySelector(`[data-highlight="${CSS.escape(id)}"]`);
    if (el) flashTracker(el);
  });
}

function flashTracker(el) {
  el.classList.remove("is-changed");
  void el.offsetWidth;
  el.classList.add("is-changed");
  const reduce = window.matchMedia("(prefers-reduced-motion: reduce)").matches;
  clearTimeout(highlightTimers.get(el));
  highlightTimers.set(
    el,
    setTimeout(() => el.classList.remove("is-changed"), reduce ? 1000 : 1100),
  );
}

// clockInZone matches Go's "3:04 PM": hour without a leading zero, and a normal space before AM/PM.
function clockInZone(date, timeZone) {
  const parts = new Intl.DateTimeFormat("en-US", {
    timeZone: timeZone || "UTC",
    hour: "numeric",
    minute: "2-digit",
    hour12: true,
  }).formatToParts(date);
  const part = (type) => parts.find((p) => p.type === type)?.value ?? "";
  return `${part("hour")}:${part("minute")} ${part("dayPeriod")}`;
}

// Invite and share links select on focus so a keyboard can copy them.
// A tap copies too: iOS will not select text in a readonly field.
document.addEventListener("focusin", (event) => {
  if (event.target.matches("input[data-select-on-focus]")) event.target.select();
});

const copiedTimers = new WeakMap();

document.addEventListener("click", (event) => {
  const input = event.target.closest("input[data-select-on-focus]");
  if (!input || !input.value) return;
  copyText(input.value).then(() => showCopied(input)).catch(() => {});
});

function copyText(text) {
  if (navigator.clipboard?.writeText) {
    return navigator.clipboard.writeText(text).catch(() => copyWithCommand(text));
  }
  return copyWithCommand(text);
}

function copyWithCommand(text) {
  const area = document.createElement("textarea");
  area.value = text;
  area.setAttribute("readonly", "");
  area.style.position = "fixed";
  area.style.opacity = "0";
  document.body.append(area);
  area.focus();
  area.select();
  area.setSelectionRange(0, text.length);
  let ok = false;
  try {
    ok = document.execCommand("copy");
  } catch {
    ok = false;
  }
  area.remove();
  return ok ? Promise.resolve() : Promise.reject(new Error("copy failed"));
}

function showCopied(input) {
  let note = input.nextElementSibling;
  if (!note?.matches("[data-copied]")) {
    note = document.createElement("small");
    note.className = "copied";
    note.setAttribute("role", "status");
    note.dataset.copied = "";
    note.textContent = "Copied to clipboard";
    input.after(note);
  }
  clearTimeout(copiedTimers.get(note));
  copiedTimers.set(
    note,
    setTimeout(() => {
      note.remove();
      copiedTimers.delete(note);
    }, 2000),
  );
}

// A closed password field must not be submitted with the email-only form.
document.querySelectorAll(".auth-password-toggle").forEach((toggle) => {
  const form = toggle.form;
  const input = form?.querySelector("[data-auth-password]");
  if (!form || !input) return;
  const linkButton = form.querySelector(".auth-link-submit");
  const passwordButton = form.querySelector(".auth-password-submit");
  const sync = () => {
    const passwordMode = toggle.checked;
    input.disabled = !passwordMode;
    // Enter and the mobile keyboard submit the first submit button.
    // Password mode has to be that button, or the press sends a magic link.
    if (linkButton && passwordButton) {
      form.action = passwordMode ? "/login" : "/login/link";
      if (passwordMode) linkButton.before(passwordButton);
      else passwordButton.before(linkButton);
    }
  };
  toggle.addEventListener("change", () => {
    if (!toggle.checked) input.value = "";
    sync();
  });
  sync();
});

// Turnstile tokens are single-use. A failed submit that stays on the form, including
// an HTMX swap of the same form, needs a new widget before the next attempt.
// The API script is deferred, so it calls onTurnstileLoad instead of turnstile.ready.
// Same URL as templates/partials/turnstile.html. A content swap of settings does not
// include the layout script, so this adds it once when a widget is on the page.
const turnstileScriptURL = "https://challenges.cloudflare.com/turnstile/v0/api.js?render=explicit&onload=onTurnstileLoad";

function renderTurnstile(root) {
  if (!window.turnstile || !root || !root.querySelectorAll) return;
  root.querySelectorAll(".cf-turnstile").forEach((el) => {
    if (el.dataset.turnstileMounted === "1") return;
    const sitekey = el.getAttribute("data-sitekey");
    if (!sitekey) return;
    el.dataset.turnstileMounted = "1";
    window.turnstile.render(el, { sitekey: sitekey, theme: "auto" });
  });
}

function ensureTurnstile(root) {
  if (!root || !root.querySelector || !root.querySelector(".cf-turnstile")) return;
  if (window.turnstile || document.querySelector("script[src*='challenges.cloudflare.com/turnstile']")) return;
  const script = document.createElement("script");
  script.src = turnstileScriptURL;
  document.head.appendChild(script);
}

// A cached history restore can put back a widget that was already mounted, or a
// dead iframe. Clear those and render again. A cache miss gets fresh markup.
function restoreTurnstile(root) {
  if (!root || !root.querySelectorAll) return;
  root.querySelectorAll(".cf-turnstile").forEach((el) => {
    if (window.turnstile) {
      try {
        window.turnstile.remove(el);
      } catch {
        // The saved node may not be a live widget. Rendering it again is enough.
      }
    }
    delete el.dataset.turnstileMounted;
    el.replaceChildren();
  });
  ensureTurnstile(root);
  renderTurnstile(root);
}

window.onTurnstileLoad = function () {
  renderTurnstile(document);
};

document.addEventListener("htmx:afterSettle", (event) => {
  const root = event.detail?.target || document;
  ensureTurnstile(root);
  renderTurnstile(root);
  syncInstallHint();
});

document.addEventListener("htmx:historyRestore", () => {
  restoreTurnstile(document.getElementById("content") || document);
});

// Home-screen install. The manifest makes the site installable; this offer
// tells a phone how. iOS has no install dialog, so the line names Share.
// Other phones get the browser menu, unless the browser offers its own prompt.
const installModes = ["install-ios", "install-menu", "install-browser", "install-prompt"];
let installPrompt = null;

function installDismissed() {
  try {
    return localStorage.getItem("install-dismissed") === "1";
  } catch {
    return false;
  }
}

function rememberInstallDismissed() {
  try {
    localStorage.setItem("install-dismissed", "1");
  } catch {
    // A private session can refuse storage. The offer still closes for this page.
  }
}

function phoneLike() {
  return window.matchMedia("(pointer: coarse)").matches;
}

function standaloneApp() {
  return window.matchMedia("(display-mode: standalone)").matches || window.navigator.standalone === true;
}

function isIos() {
  if (/iPad|iPhone|iPod/.test(navigator.userAgent)) return true;
  // iPadOS reports itself as a Mac, and a real Mac has no touch points.
  return navigator.platform === "MacIntel" && navigator.maxTouchPoints > 1;
}

function inAppBrowser() {
  return /Instagram|FBAN|FBAV|FB_IAB|Line\/|Snapchat|TikTok|BytedanceWebview|LinkedInApp|Twitter/i.test(navigator.userAgent);
}

function clearInstallOffer() {
  document.documentElement.classList.remove("install-offer", ...installModes);
}

function syncInstallHint() {
  if (!document.querySelector("[data-install-hint]") || !phoneLike() || standaloneApp() || installDismissed()) {
    clearInstallOffer();
    return;
  }
  let mode = "install-menu";
  if (installPrompt) mode = "install-prompt";
  else if (inAppBrowser()) mode = "install-browser";
  else if (isIos()) mode = "install-ios";
  const root = document.documentElement;
  for (const name of installModes) root.classList.toggle(name, name === mode);
  root.classList.add("install-offer");
}

window.addEventListener("beforeinstallprompt", (event) => {
  if (!document.querySelector("[data-install-hint]") || !phoneLike() || standaloneApp() || installDismissed()) return;
  event.preventDefault();
  installPrompt = event;
  syncInstallHint();
});

window.addEventListener("appinstalled", () => {
  installPrompt = null;
  rememberInstallDismissed();
  clearInstallOffer();
});

document.addEventListener("click", (event) => {
  if (event.target.closest("[data-install-dismiss]")) {
    rememberInstallDismissed();
    clearInstallOffer();
    return;
  }
  if (!event.target.closest("[data-install-action]") || !installPrompt) return;
  const prompt = installPrompt;
  installPrompt = null;
  prompt.prompt();
  prompt.userChoice.finally(() => syncInstallHint());
});

syncInstallHint();
