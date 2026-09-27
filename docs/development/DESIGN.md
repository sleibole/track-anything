# Track Anything — Design

How Track Anything should look and behave. Product scope is `PLAN.md`. Implementation is `ARCHITECTURE.md`. User help, when it exists, belongs in `docs/help/`.

This is a small, practical guide, not a formal design system. Pico.css supplies type, spacing, color, buttons, and cards. Custom CSS is for the cases Pico does not already cover.

## Visual language

- **Calm notebook, mobile-first.** Fast to log, easy to review, and the same card pattern for the dog's meals now and a workout measurement later. A family calendar is loose inspiration for recognizable icons, soft colors, and approachable cards. Keep each screen less crowded than that: one primary action, the name always readable, no illustrated scenes, and no gradients.
- **Pico, classless-first.** Reach for Pico's elements before adding classes. Keep `app.css` small.
- **Pico's color, including dark mode.** Pico is the only UI palette. The brand blue is Pico's primary (`#0172ad` in the install theme color). The app icon is Tabler tally marks on that blue. A tracker may add one optional accent from a small built-in set of soft tints, on its card and icon only. The accent helps recognition and is never the only label. It has to stay clear in light and dark. Chrome icons use `currentColor` so they follow text color, including dark mode and button hover.
- **Obvious in a glance.** No decorative chrome. A tracker card is a name, an icon, a summary, and a button.

## Page layout

The site header stays on every page, including authentication. Page content sits in Pico's container, under that header, with ordinary spacing. Do not vertically center page content.

Dashboards, tracker history, and charts use the normal content width and align with the container. They are not squeezed into a form card. Home is a short stack of tracker cards in that width, not a calendar grid.

## Screens

Build the narrow screen first. The logging rules are in **Recording** below. Permissions are in `PLAN.md`.

### Home

Logged-in home keeps the household groups. Inside a group, each tracker is one card:

- The name, always visible.
- The chosen icon, or the tally-mark default.
- An optional accent, as a soft tint on the card or the icon. No accent means an ordinary Pico card.
- A useful summary, such as "2 times today", "None today", or "Nothing logged today".
- One prominent logging button. On the dog card that button is **+ Ate**. The default label is **+ Log**. One tap records the current time and then offers **Undo**.

Cards stack in a single column. They do not tile into a dense grid. On a wide screen they may sit in a quiet row, still with room around them and with the name and the button readable.

Each group's heading is the household name. A household someone else created adds that person's email in quieter text, so a partner's "My trackers" doesn't look like your own.

### Tracker detail

The header repeats the icon and the name, with the accent if there is one.

Order on a phone:

1. Today's logging control: the log button, then a small form with an optional note. Owners also see a time field, set to now, for backfill. Members don't see it.
2. Today's entries. Each one can be undone while it is recent. An owner can edit the time or note in place, or delete with confirmation.
3. History.
4. The trend, once that tracker has one. Counts arrive in phase 3. Weight and duration arrive in phase 4.

On a wider screen, history and the trend may sit beside the logging column. They do not move above it. Logging does not require scrolling past a chart.

**Record none** is a quiet text action, not a second primary button. It marks today as a recorded zero when today has no events yet. "None today" and "Nothing logged today" stay visually distinct in the summary and in history.

### Household

The household name, then its members with their roles. An owner sees the invite link controls, and **Remove** and **Make owner** on each member's row. Owner rows, including your own, have no remove control. Members see the list without those controls.

Below that, the household's trackers. An owner sees **New tracker**, which opens the create form already set to this household. Members don't.

An owner also sees a collapsed **Archived trackers** section (a `<details>` element), closed by default and left out when nothing is archived. Each archived tracker shows its icon and name with a quiet **Restore** action. There is no separate archive page.

### History

Show the date, then that day's entries with the time they happened. A recorded zero reads as none. A day with no entries and no recorded zero reads as nothing logged. Those two states do not look the same.

Who logged an entry can be shown where it helps, without putting it on every line.

### Trends

Choose the chart from the tracker's data:

- Count: events per day, such as meals.
- Number: the value over time, such as workout weight.
- Duration: length over time.

A recorded zero is a zero. A day with nothing logged is a gap, not a zero bar.

The historical chart is on the tracker page. Overlaying another tracker comes after that, on the phase 3 charts view. Events from the second tracker sit on the first tracker's time series as a marker, an annotation, or a shaded vertical region. Which of those is still open (`PLAN.md`). Woods outings on the meals chart are the reference. The page describes the chart and does not claim a cause. On a phone the chart follows the overlay controls. On a wider screen it can sit beside them.

### Icon and accent

The create and edit forms offer a small picker. It contains a curated handful of clear Tabler icons — a paw, a pill, trees, a barbell — and a few emoji. It is not the whole Tabler set, not a free-text emoji field, and not an upload. The tally-mark icon is selected by default. The owner can change the icon or the accent later.

Show the icon on the home card, the tracker header, and every control that picks a tracker, including the phase 3 overlay picker. The name stays beside it. Color and icon help recognition. They are not the only way to identify a tracker.

Uploading an icon is a later idea.

## Focused forms

Focused form cards are horizontally centered by default unless there is a compelling reason for them not to be.

That covers authentication, settings, create and edit forms, and any other single-purpose card. The card has a max width that fits the form (auth cards are about 30rem). It does not stretch across the page, and it is not left-aligned just because the content container starts on the left. It sits below the header with comfortable spacing. It is not vertically centered. On a small screen it uses the available width, with the page's usual margins.

A form can sit to one side when the leftover space is doing something: accompanying copy, another panel, a chart, or a preview.

The new-tracker form is one of these cards. The name, the icon picker, the optional accent, and the log label sit in it. The primary button is still the single full-width action that creates the tracker.

Inside a focused form:

- One clear heading.
- Labels above fields.
- Compact spacing. The fields and the primary button dominate the card.
- One full-width primary button. Secondary actions are quiet text, not competing buttons.
- A visible focus ring on fields, buttons, and links.
- Errors in text above the fields. No extra explanation unless the words prevent a mistake.

## Authentication

Authentication should have almost no cognitive overhead.

- Passwordless email is the primary path. Signup and login open on an email field and a **Continue** button.
- A password is a secondary step ("Sign up with a password instead", "Log in with a password instead"), revealed only after the person asks for it. Do not show both methods at once.
- Do not add Google, Apple, or other social login unless a later product decision says to.
- Keep the normal site header. Do not use a split-screen marketing layout; there is not enough branding to justify one.
- The "check your email" and "log in as …" screens use the same centered card.

## Recording

Recording stays faster than reading.

- The log button posts in place, updates today's summary immediately, and shows a short-lived **Undo**. The server response confirms the summary. Undo disappears once the 15-minute window has passed. The default label is **+ Log**. A tracker can use a short custom label such as **+ Ate**. The tap still records the current time.
- **Record none** is a quiet action, not a second primary button. It marks today as a recorded zero when today has no events. The summary then says "None today", which is different from "Nothing logged today".
- Logging with a note, a time (owners), or a value (phase 4) adds the new row and leaves the form ready for the next one, prefilled from what was just logged. The note does not carry forward onto the next entry, and the time goes back to now.
- Controls a person can't use aren't shown. Members get the note field but no time field. Share-link visitors get the log button, **Record none**, and **Undo**.
- Owners edit an entry's time or note in place. Delete asks for confirmation. Those controls sit on the entry. Members and share links still only undo while the entry is recent.
- **Archive** is a quiet owner action on the tracker's edit form, not a button on the card. Restoring happens on the household page.
- A trend never sits above the log control, and the log control works without the chart script. Chart pages describe a pattern and never claim a cause.
- Empty and early screens say what is missing in one or two sentences and offer the next action. They do not lecture.

## Navigation

Logged out: **Log in** and **Sign up** in the header. Logged in: settings and log out. The brand mark links home. Icon-only controls in the header need an accessible name (settings, log out).

## Icons

[Tabler Icons](https://tabler.io/icons) (MIT). Use an icon only where it helps comprehension; a text label is fine. Icon-only buttons carry an accessible name. Decorative icons are hidden from assistive tech. How they are embedded is in `ARCHITECTURE.md`.

Tabler instead of Font Awesome: an icon font or a JavaScript loader is the wrong trade here, and Font Awesome's CC BY license was a worse fit.

Chrome icons, the ones the app itself uses: `tallymarks` (the brand mark, also the app icon and the default tracker icon), `plus`, `arrow-back-up` (undo), `trash`, `pencil`, `chart-line`, `settings`, `share`, `users`, `archive`, `calendar`, `link`, `logout`. Form labels add `mail` (email), `lock` (password), `lock-check` (confirm password), and `timezone`.

The tracker picker is a separate handful, described under **Screens**. Copy in a Tabler SVG when it joins that picker. Emoji in the picker are text, not SVGs. Do not offer the whole Tabler set, and do not accept an uploaded icon in the first version.

## Ads

When ads exist, they stay out of share, login, and settings pages, and out of the view for anyone on the ad-free plan (including the grace period). They must not reappear or reload when part of the page updates in place.

## Responsive behavior

Design the narrow screen first. There is still one web layout and no native app. Home-screen install is a manifest, specified in `ARCHITECTURE.md`.

On a phone, tracker cards stack, and the logging control is the first content on a tracker page. History and trends follow, so reviewing is a scroll down the same page.

On a wider screen, focused forms stay narrow and centered. A tracker page may place history and trends beside the logging column, inside the normal content width. A chart does not take the place of the log control.

Forms and cards use the width they have, with the page margins Pico already applies.
