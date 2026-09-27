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

// The log button updates the summary on tap. The server's page replaces it moments later.
document.addEventListener("submit", (event) => {
  const form = event.target.closest("form[data-quick]");
  const summary = form?.closest("[data-tracker]")?.querySelector("[data-summary]");
  if (!summary) return;
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
  summary.textContent = count === 1 ? "1 time today" : `${count} times today`;
});

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

// Invite and share links select on focus so they're easy to copy.
document.addEventListener("focusin", (event) => {
  if (event.target.matches("input[data-select-on-focus]")) event.target.select();
});

// A closed password field must not be submitted with the email-only form.
document.querySelectorAll(".auth-password-toggle").forEach((toggle) => {
  const input = toggle.form?.querySelector("[data-auth-password]");
  if (!input) return;
  const sync = () => {
    input.disabled = !toggle.checked;
  };
  toggle.addEventListener("change", () => {
    if (!toggle.checked) input.value = "";
    sync();
  });
  sync();
});
