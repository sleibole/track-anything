// Small browser behaviors that HTMX doesn't cover. Keep this file short.

// HTMX updates the document title from the fragment and leaves <head> in place.
// The fragment carries this page's canonical, description, social, and robots
// tags in an inert template. Copy them into <head> after a content swap and
// when history restores that fragment.
function syncPageMeta() {
  const holder = document.querySelector("#content template[data-page-meta]");
  if (!holder) return;
  document.head.querySelectorAll("[data-page-meta]").forEach((node) => node.remove());
  holder.content.querySelectorAll("[data-page-meta]").forEach((node) => {
    document.head.appendChild(node.cloneNode(true));
  });
}

document.addEventListener("htmx:afterSwap", (event) => {
  if (event.detail?.target?.id === "content") syncPageMeta();
});
document.addEventListener("htmx:historyRestore", syncPageMeta);

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
  syncInstall();
});

document.addEventListener("htmx:historyRestore", () => {
  restoreTurnstile(document.getElementById("content") || document);
  syncInstall();
});

// Install help. The early listener in the layout stores beforeinstallprompt.
// Nothing is written to localStorage: this window's display mode, plus
// appinstalled for the current document, decides whether the footer is shown.
const installDisplayModes = ["standalone", "fullscreen", "minimal-ui", "window-controls-overlay"];
let chosenPlatform = "";
let installOpener = null;

function runningInstalled() {
  const state = window.trackInstall;
  if (state && typeof state.runningInstalled === "function") return state.runningInstalled();
  for (const mode of installDisplayModes) {
    if (window.matchMedia(`(display-mode: ${mode})`).matches) return true;
  }
  return window.navigator.standalone === true;
}

function invitationHidden() {
  const state = window.trackInstall;
  return runningInstalled() || !!(state && state.sessionInstalled);
}

function usablePrompt(event) {
  return !!event && typeof event.prompt === "function";
}

function detectPlatform() {
  const ua = navigator.userAgent || "";
  // iPadOS can report a desktop Mac. Chrome on iOS still carries iPhone or iPad.
  const iOS = /iPad|iPhone|iPod/.test(ua) || (navigator.platform === "MacIntel" && navigator.maxTouchPoints > 1);
  if (iOS) return "ios";
  if (/Android/i.test(ua)) return "android";
  if (/Windows|Macintosh|Mac OS X|Linux|CrOS|X11/i.test(ua)) return "desktop";
  return "unknown";
}

function selectedPlatform() {
  if (chosenPlatform === "ios" || chosenPlatform === "android" || chosenPlatform === "desktop") return chosenPlatform;
  return detectPlatform();
}

function inAppBrowser() {
  return /Instagram|FBAN|FBAV|FB_IAB|Line\/|Snapchat|TikTok|BytedanceWebview|LinkedInApp|Twitter|MicroMessenger|Pinterest|WhatsApp|GSA\//i.test(navigator.userAgent || "");
}

// Only Safari on iPhone and iPad can add the Home Screen web app. Brave is
// named when the user agent says so. Other iOS browsers, and an in-app page,
// get the same Safari handoff without naming a browser. A computer that
// shows the iPhone steps keeps the Safari list.
function iosHandoff() {
  if (detectPlatform() !== "ios") return "";
  const ua = navigator.userAgent || "";
  if (/Brave/i.test(ua)) return "brave";
  if (/CriOS|FxiOS|EdgiOS|OPiOS|DuckDuckGo/i.test(ua) || inAppBrowser()) return "other";
  return "";
}

function safeInstallURL() {
  try {
    const url = new URL("/", location.origin);
    if (url.username || url.password || url.search || url.hash) return "";
    if (url.origin !== location.origin) return "";
    return url.href;
  } catch {
    return "";
  }
}

function fillInstallURL(dialog) {
  const node = dialog.querySelector("[data-install-url]");
  if (node) node.textContent = safeInstallURL();
}

function installURLLine(dialog) {
  return dialog.querySelector("[data-install-url-line]");
}

function announceInstall(message, kind) {
  const el = document.querySelector("#install-dialog [data-install-status]");
  if (!el) return;
  el.classList.toggle("error", kind === "error");
  el.textContent = "";
  el.textContent = message;
}

function renderInstallDialog(dialog) {
  const state = window.trackInstall || {};
  const usable = usablePrompt(state.event) && !state.prompting;
  const native = dialog.querySelector("[data-install-native]");
  if (native) native.hidden = !usable;
  const toggleWrap = dialog.querySelector("[data-install-other-wrap]");
  const toggle = dialog.querySelector("[data-install-toggle]");
  const manual = dialog.querySelector("[data-install-manual]");
  const expanded = toggle && toggle.getAttribute("aria-expanded") === "true";
  if (toggleWrap) toggleWrap.hidden = !usable;
  if (manual) manual.hidden = usable && !expanded;
  const kind = selectedPlatform();
  dialog.querySelectorAll("[data-install-panel]").forEach((panel) => {
    panel.hidden = panel.dataset.installPanel !== kind;
  });
  const handoff = kind === "ios" ? iosHandoff() : "";
  const safariSteps = dialog.querySelector("[data-install-ios='safari']");
  const handoffSteps = dialog.querySelector("[data-install-ios='handoff']");
  const braveNote = dialog.querySelector("[data-install-brave]");
  const otherNote = dialog.querySelector("[data-install-ios-other]");
  if (safariSteps) safariSteps.hidden = handoff !== "";
  if (handoffSteps) handoffSteps.hidden = handoff === "";
  if (braveNote) braveNote.hidden = handoff !== "brave";
  if (otherNote) otherNote.hidden = handoff !== "other";
  const known = kind === "ios" || kind === "android" || kind === "desktop";
  const unknown = dialog.querySelector("[data-install-unknown]");
  if (unknown) unknown.hidden = known;
  const inApp = inAppBrowser();
  const inAppNote = dialog.querySelector("[data-install-inapp]");
  if (inAppNote) inAppNote.hidden = !inApp;
  const copy = dialog.querySelector("[data-install-copy]");
  const showCopy = kind === "ios" || inApp;
  if (copy) copy.hidden = !showCopy;
  if (showCopy) fillInstallURL(dialog);
  dialog.querySelectorAll("[data-install-choose]").forEach((button) => {
    button.setAttribute("aria-pressed", button.dataset.installChoose === kind ? "true" : "false");
  });
}

function syncInstall() {
  const hidden = invitationHidden();
  document.documentElement.classList.toggle("install-session", hidden);
  const footer = document.querySelector("[data-install-footer]");
  if (footer) footer.hidden = hidden;
  const dialog = document.getElementById("install-dialog");
  if (!dialog) return;
  if (hidden && runningInstalled() && dialog.open) dialog.close();
  renderInstallDialog(dialog);
}

function lockInstallScroll() {
  const body = document.body;
  if (body.dataset.installScroll) return;
  const y = window.scrollY;
  const bar = window.innerWidth - document.documentElement.clientWidth;
  body.dataset.installScroll = String(y);
  body.style.position = "fixed";
  body.style.top = `-${y}px`;
  body.style.left = "0";
  body.style.right = "0";
  body.style.paddingRight = bar > 0 ? `${bar}px` : "";
}

function unlockInstallScroll() {
  const body = document.body;
  if (!body.dataset.installScroll) return null;
  const y = Number(body.dataset.installScroll);
  body.style.position = "";
  body.style.top = "";
  body.style.left = "";
  body.style.right = "";
  body.style.paddingRight = "";
  delete body.dataset.installScroll;
  return Number.isFinite(y) ? y : 0;
}

let installReleasing = false;

function releaseInstallDialog() {
  if (installReleasing) return;
  installReleasing = true;
  try {
    const y = unlockInstallScroll();
    const opener = installOpener;
    installOpener = null;
    if (opener && opener.isConnected && !opener.hidden && opener.offsetParent !== null) opener.focus();
    if (y != null) window.scrollTo(0, y);
  } finally {
    installReleasing = false;
  }
}

function bindInstallDialog(dialog) {
  if (dialog.dataset.installBound === "1") return;
  dialog.dataset.installBound = "1";
  // This browser fires toggle and not close when a dialog shuts.
  const onClosed = (event) => {
    if (event.type === "toggle" && event.newState !== "closed") return;
    releaseInstallDialog();
  };
  dialog.addEventListener("toggle", onClosed);
  dialog.addEventListener("close", onClosed);
  dialog.addEventListener("click", (event) => {
    if (event.target === dialog) dialog.close();
  });
}

function openInstallDialog(trigger) {
  const dialog = document.getElementById("install-dialog");
  if (!dialog || invitationHidden()) return;
  bindInstallDialog(dialog);
  installOpener = trigger instanceof HTMLElement ? trigger : null;
  const status = dialog.querySelector("[data-install-status]");
  if (status) {
    status.textContent = "";
    status.classList.remove("error");
  }
  const urlLine = installURLLine(dialog);
  if (urlLine) urlLine.hidden = true;
  syncInstall();
  if (dialog.open) return;
  lockInstallScroll();
  try {
    dialog.showModal();
  } catch {
    const y = unlockInstallScroll();
    if (y != null) window.scrollTo(0, y);
    return;
  }
  const prompt = dialog.querySelector("[data-install-prompt]");
  const native = dialog.querySelector("[data-install-native]");
  const close = dialog.querySelector("[data-install-close]");
  if (native && !native.hidden && prompt) prompt.focus();
  else if (close) close.focus();
}

function startPrompt() {
  const state = window.trackInstall;
  if (!state || state.prompting || !usablePrompt(state.event)) return;
  const event = state.event;
  state.event = null;
  state.prompting = true;
  syncInstall();
  let settled = false;
  let reported = false;
  const finish = () => {
    if (settled) return;
    settled = true;
    state.prompting = false;
    syncInstall();
  };
  const fail = () => {
    if (reported) return;
    reported = true;
    announceInstall("The install prompt could not be opened. Use the steps below.", "error");
  };
  try {
    const prompted = event.prompt();
    if (prompted && typeof prompted.catch === "function") prompted.catch(fail);
    const choice = event.userChoice;
    if (!choice || typeof choice.then !== "function") {
      fail();
      finish();
      return;
    }
    choice.then((result) => {
      if (reported) return;
      reported = true;
      if (result && result.outcome === "accepted") {
        announceInstall("The browser is handling installation. This tab stays open.");
      } else {
        announceInstall("Install prompt closed.");
      }
    }).catch(fail).finally(finish);
  } catch {
    fail();
    finish();
  }
}

function showInstallURL(dialog) {
  fillInstallURL(dialog);
  const line = installURLLine(dialog);
  if (line) line.hidden = false;
  return dialog.querySelector("[data-install-url]");
}

function copyInstallLink() {
  const dialog = document.getElementById("install-dialog");
  if (!dialog) return;
  const url = safeInstallURL();
  if (!url) {
    announceInstall("Could not copy the link. Select the address below and copy it.", "error");
    const node = showInstallURL(dialog);
    if (node) node.tabIndex = -1;
    return;
  }
  copyText(url).then(() => {
    const line = installURLLine(dialog);
    if (line) line.hidden = true;
    announceInstall("Link copied.");
  }).catch(() => {
    announceInstall("Could not copy the link. Select the address below and copy it.", "error");
    const node = showInstallURL(dialog);
    if (!node) return;
    node.tabIndex = -1;
    node.focus();
    const range = document.createRange();
    range.selectNodeContents(node);
    const selection = window.getSelection();
    if (!selection) return;
    selection.removeAllRanges();
    selection.addRange(range);
  });
}

if (!window.trackInstall || !window.trackInstall.uiBound) {
  window.trackInstall = window.trackInstall || { event: null, prompting: false, sessionInstalled: false, notice: "" };
  window.trackInstall.uiBound = true;
  if (!window.trackInstall.listening) {
    window.trackInstall.listening = true;
    window.trackInstall.runningInstalled = runningInstalled;
    window.addEventListener("beforeinstallprompt", (event) => {
      if (runningInstalled() || typeof event.prompt !== "function") return;
      event.preventDefault();
      window.trackInstall.event = event;
      window.trackInstall.sessionInstalled = false;
      window.trackInstall.notice = "";
      document.documentElement.classList.remove("install-session");
      document.dispatchEvent(new Event("track-install"));
    });
    window.addEventListener("appinstalled", () => {
      window.trackInstall.event = null;
      window.trackInstall.sessionInstalled = true;
      window.trackInstall.notice = "installed";
      document.documentElement.classList.add("install-session");
      document.dispatchEvent(new Event("track-install"));
    });
  }
  for (const mode of installDisplayModes) {
    const mq = window.matchMedia(`(display-mode: ${mode})`);
    const onChange = () => syncInstall();
    if (typeof mq.addEventListener === "function") mq.addEventListener("change", onChange);
    else if (typeof mq.addListener === "function") mq.addListener(onChange);
  }
  document.addEventListener("track-install", () => {
    const state = window.trackInstall;
    if (state && state.notice === "installed") {
      state.notice = "";
      const dialog = document.getElementById("install-dialog");
      if (dialog && dialog.open) announceInstall("This browser finished adding Track Anything.");
    }
    syncInstall();
  });
  document.addEventListener("htmx:beforeSwap", (event) => {
    const dialog = document.getElementById("install-dialog");
    const target = event.detail && event.detail.target;
    if (!dialog || !dialog.open || !target || typeof target.contains !== "function") return;
    if (target === dialog || target.contains(dialog)) dialog.close();
  });
  window.addEventListener("pageshow", () => syncInstall());
  document.addEventListener("click", (event) => {
    const open = event.target.closest("[data-install-open]");
    if (open) {
      openInstallDialog(open);
      return;
    }
    if (event.target.closest("[data-install-close]")) {
      document.getElementById("install-dialog")?.close();
      return;
    }
    if (event.target.closest("[data-install-prompt]")) {
      startPrompt();
      return;
    }
    if (event.target.closest("[data-install-copy-button]")) {
      copyInstallLink();
      return;
    }
    const choose = event.target.closest("[data-install-choose]");
    if (choose) {
      chosenPlatform = choose.dataset.installChoose || "";
      syncInstall();
      return;
    }
    const toggle = event.target.closest("[data-install-toggle]");
    if (toggle) {
      const expanded = toggle.getAttribute("aria-expanded") === "true";
      const manual = document.getElementById("install-manual");
      if (expanded && manual && manual.contains(document.activeElement)) toggle.focus();
      toggle.setAttribute("aria-expanded", expanded ? "false" : "true");
      syncInstall();
    }
  });
}

syncInstall();
