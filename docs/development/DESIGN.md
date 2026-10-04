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
- A summary line, from that tracker's summary display (`PLAN.md`). The log label does not choose this line.
  - **Times today** (the default): "Nothing logged today", "1 time today", "3 times today", or "None today".
  - **Done today**: "Done today" when today has at least one entry, "Not done today" otherwise. A second entry today does not change the line.
  - **Last occurrence**: "Last: 6:42 AM" when the latest entry is today, "Last: Yesterday, 8:15 PM" when it was yesterday, "Last: Mon, Jan 2, 8:15 PM" when it was earlier, or "Never logged" when the tracker has no entries. After midnight, today's time becomes "Last: Yesterday, …". It does not become "Nothing logged today".
- One prominent logging button. On the Millie card that button is **+ Ate**. Logan Motrin can say **Gave Motrin**. The default label is **+ Log**. One tap records the current time and then offers **Undo**. The button text is the log label, not the summary.

Cards stack in a single column. They do not tile into a dense grid. On a wide screen they may sit in a quiet row, still with room around them and with the name and the button readable.

Each group's heading is the household name, and usually nothing else. When two households on the page have the same name, after trimming surrounding whitespace and ignoring case, the one someone else created adds that person's email in quieter text, so a partner's "My trackers" doesn't look like your own. Your own households never carry your email, and you cannot own two that match that way. "My trackers" is just the default name for a personal household. It does not suggest a shared family space.

Below the groups, a quiet **New household** action (a `<details>` element, closed by default) opens one name field and a **Create** button. A name that matches another household you already own, after trimming surrounding whitespace and ignoring case, stays on the form with an error and creates nothing. Creating one lands on the new household's page, where the invite link is. Home does not turn into a household admin screen.

The header's global **New tracker** keeps its household picker. Starting from a household page is still the preferred path, because it opens the form already set to that household.

### Tracker detail

The header repeats the icon and the name, with the accent if there is one.

Order on a phone:

1. Today's logging control: the summary line, then the log button, then a small form with an optional note. The summary is the same line as on the card. Owners also see a time field, set to now, for backfill. Members don't see it.
2. Today's entries. Each one can be undone while it is recent. An owner can edit the time or note in place, or delete with confirmation.
3. The trend. A count tracker shows a line of the last 30 local days once any of those days has a count or a recorded zero. Otherwise one sentence says nothing was logged in that window. The heading is Trend. A quiet control under the heading can mark one other tracker's events on that line. Weight and duration arrive in phase 4.
4. History.

On a wider screen, the trend and history may sit beside the logging column, with the trend above history. They do not move above the log control. Logging does not require scrolling past a chart.

**Record none** is a quiet text action, not a second primary button. It marks today as a recorded zero when today has no events yet. On a Times today summary, "None today" and "Nothing logged today" stay visually distinct. Done today stays "Not done today" for both, because a recorded zero is not an entry. Last occurrence does not move. History still shows none and nothing logged as different rows. The today-list's empty sentence ("Nothing logged yet") is separate from the summary line, including "Never logged".

### Household

The household name, then its members with their roles. An owner sees the invite link controls, and **Remove** and **Make owner** on each member's row. Owner rows, including your own, have no remove control. Members see the list without those controls.

Below that, the household's trackers. An owner sees **New tracker**, which opens the create form already set to this household. Members don't.

An owner also sees a collapsed **Archived trackers** section (a `<details>` element), closed by default and left out when nothing is archived. Each archived tracker shows its icon and name with a quiet **Restore** action. There is no separate archive page.

Two quiet controls sit on this page:

- **Rename**, for owners, next to the heading. It is a `<details>` element with the name field and a **Save** button. It is not a separate settings page. A personal "My trackers" can be renamed the same way. A name that matches another household you own stays on the form with an error and changes nothing.
- **Leave household**, for regular members, as quiet text at the bottom of the page. It asks for confirmation and then returns home. Owners don't see it.

The heading is the household name. The member list already shows who is in it. The join page still says who sent the invitation.

### History

Show the date, then that day's entries with the time they happened. A recorded zero reads as none. A day with no entries and no recorded zero reads as nothing logged. Those two states do not look the same.

Who logged an entry can be shown where it helps, without putting it on every line.

### Trends

Choose the chart from the tracker's data:

- Count: events per day, such as meals.
- Number: the value over time, such as workout weight.
- Duration: length over time.

A recorded zero is a zero. A day with nothing logged is a gap, not a zero.

The historical count chart is on the tracker page, under the heading Trend. It is a line of events per day for the last 30 local days, oldest at the left, including today. A recorded zero is a point at zero. A day with nothing logged is a gap in the line, not a zero. If none of those days has a count or a recorded zero, the page says "Nothing logged in the last 30 days." and does not draw a chart. Week and month views are not on the page. Number and duration charts are phase 4.

One other tracker can be drawn on that same trend. The control sits with the Trend heading: **Show events from**, closed until opened, with **None** and each other count tracker the person can see. Each choice shows that tracker's icon and name. Choosing one reloads this page with `?overlay=`. Choosing **None** removes it. The choice is not saved.

An overlay day is a narrow translucent vertical band across the plot, in Pico's primary color at low opacity, so it stays visible in light and dark and stays quieter than the count line. A short cap at the top of the band marks the day. The band is not a duration. Several events on one local day are one band. Hover or tap names the overlay tracker and the local date, and lists the times when there are more than one. If the overlay has no events in the window, the count line stays and the page says "No Woods run events in the last 30 days." An overlay does not draw a chart when the primary tracker has nothing logged in the window. The page describes the bands and does not claim a cause. There is no separate charts page. Woods outings on the meals chart are the reference.

### Icon and accent

The create and edit forms offer a small picker. It contains a curated handful of clear Tabler icons — a paw, a pill, trees, a barbell — and a few emoji. It is not the whole Tabler set, not a free-text emoji field, and not an upload. The tally-mark icon is selected by default. The owner can change the icon or the accent later.

Show the icon on the home card, the tracker header, and every control that picks a tracker, including the overlay picker. The name stays beside it. Color and icon help recognition. They are not the only way to identify a tracker.

Uploading an icon is a later idea.

## Focused forms

Focused form cards are horizontally centered by default unless there is a compelling reason for them not to be.

That covers authentication, settings, create and edit forms, and any other single-purpose card. The card has a max width that fits the form (auth cards are about 30rem). It does not stretch across the page, and it is not left-aligned just because the content container starts on the left. It sits below the header with comfortable spacing. It is not vertically centered. On a small screen it uses the available width, with the page's usual margins.

A form can sit to one side when the leftover space is doing something: accompanying copy, another panel, a chart, or a preview.

The new-tracker form is one of these cards. The name, the icon picker, the optional accent, the log label, and the summary display sit in it. Summary display is three ordinary radios, not the icon picker:

- **Times today** — show how many times it happened today
- **Done today** — show whether it happened today
- **Last occurrence** — show when it last happened

Times today is selected by default. It is not tied to the log label. Edit offers the same choice. The primary button is still the single full-width action that creates the tracker.

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
- A password is optional and is set in Settings after the email is confirmed. Signup does not ask for one. On the login page it is a secondary step ("Log in with a password instead"), revealed only after the person asks for it. Do not show both login methods at once.
- Do not add Google, Apple, or other social login unless a later product decision says to.
- Keep the normal site header. Do not use a split-screen marketing layout; there is not enough branding to justify one.
- The "check your email" and "log in as …" screens use the same centered card.

## Recording

Recording stays faster than reading.

- The log button posts in place, updates the summary line immediately, and shows a short-lived **Undo**. The server response confirms the summary. The update follows the summary display: a new Times today count, "Done today", or "Last" at the current time. Undo disappears once the 15-minute window has passed. The default label is **+ Log**. A tracker can use a short custom label such as **+ Ate** or **Gave Motrin**. The tap still records the current time. Another tap on a Done today tracker records another entry.
- **Record none** is a quiet action, not a second primary button. It marks today as a recorded zero when today has no events. A Times today summary then says "None today", which is different from "Nothing logged today". Done today stays "Not done today". Last occurrence keeps the latest entry.
- Logging with a note, a time (owners), or a value (phase 4) adds the new row and leaves the form ready for the next one, prefilled from what was just logged. The note does not carry forward onto the next entry, and the time goes back to now.
- Home, the tracker page, and the share page stay current on their own: every 30 seconds, and when the app comes back to the front, they quietly reload their content, so a partner's log shows up without a refresh. They never reload while someone is typing in a field or has a section such as **Log with a note** or an entry's **Edit** open. There is no spinner or banner for it.
- Controls a person can't use aren't shown. Members get the note field but no time field. Share-link visitors get the summary line, the log button, **Record none**, and **Undo**. The summary is the same line as on the card.
- Owners edit an entry's time or note in place. Delete asks for confirmation. Those controls sit on the entry. Members and share links still only undo while the entry is recent.
- **Archive** is a quiet owner action on the tracker's edit form, not a button on the card. Restoring happens on the household page.
- **Move to household** sits beside Archive on the edit form. It only appears when the owner owns another household. It is a small select listing those households, with a **Move** button and a confirmation. The confirmation names the destination and says who the move affects: members of the current household who are not in the destination lose access, and members of the destination gain access, including the tracker's history. When the tracker has an active share link, the confirmation also says that link will keep working. The move does not turn the link off or replace it. Then it returns to the tracker page. The tracker keeps its name, icon, accent, log label, summary display, entries, and share link.
- A trend never sits above the log control, and the log control works without the chart script. Chart pages describe a pattern and never claim a cause.
- Empty and early screens say what is missing in one or two sentences and offer the next action. They do not lecture.

Offline logging is a later enhancement (`PLAN.md`). The log button stays one tap. The offline path adds no confirm step, no second button, and no queue screen.

- A tap while offline records the current time and updates the summary immediately. The line follows the summary display, same as an online tap: the next Times today count, "Done today", or "Last" at that time. If today's entries are already on the page, the new row appears there too.
- The card adds a quiet status next to that summary: "Waiting to sync", or "Not synced" when the send keeps failing. The same words sit on a pending row. The status does not replace the summary, move the log button, or cover the card.
- **Undo** stays hidden while the entry is still queued. After it syncs, the usual short-lived Undo appears.
- The next tap stays the primary action. The unsynced state stays visible and stays out of the way.
- A note, a chosen time, record none, and editing a value stay on the online form.
- History, charts, create and edit, households, settings, and account screens stay online. This enhancement does not add offline versions of them.

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

Design the narrow screen first. There is still one web layout and no native app. On a phone, the logged-out home and the dashboard show a short offer to add the app to the device home screen. It can be dismissed. The manifest is specified in `ARCHITECTURE.md`. Offline logging, later, keeps this same log button.

On a phone, tracker cards stack, and the logging control is the first content on a tracker page. The trend follows, then history, so reviewing is a scroll down the same page.

On a wider screen, focused forms stay narrow and centered. A tracker page may place the trend and history beside the logging column, with the trend above history, inside the normal content width. A chart does not take the place of the log control.

Forms and cards use the width they have, with the page margins Pico already applies.
