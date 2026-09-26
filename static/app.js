// Small browser behaviors that HTMX doesn't cover. Keep this file short.

const browserTimeZone = Intl.DateTimeFormat().resolvedOptions().timeZone || "UTC";

// Hidden fields on signup get the browser's IANA time zone.
document.querySelectorAll("input[data-browser-timezone]").forEach((input) => {
  input.value = browserTimeZone;
});

// Settings: a button that fills in this device's time zone.
document.querySelectorAll("button[data-use-browser-timezone]").forEach((button) => {
  const input = button.form.elements.timezone;
  button.textContent = `Use this device's time zone (${browserTimeZone})`;
  button.hidden = input.value === browserTimeZone;
  button.addEventListener("click", () => {
    input.value = browserTimeZone;
    button.hidden = true;
  });
});
