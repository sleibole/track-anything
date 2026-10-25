# Track Anything — Design

How Track Anything should look and behave. Product scope is `PLAN.md`. Implementation is `ARCHITECTURE.md`. User help, when it exists, belongs in `docs/help/`.

This is a small, practical guide, not a formal design system. Pico.css supplies type, spacing, buttons, and cards. Brand color lives in `static/theme.css`. Other custom CSS is for the cases Pico does not already cover.

## Visual language

- **Calm notebook, mobile-first.** Fast to log, easy to review, and the same card pattern for the dog's meals now and a workout measurement later. A family calendar is loose inspiration for recognizable icons, soft colors, and approachable cards. Keep each screen less crowded than that: one primary action, the name always readable, no illustrated scenes, and no gradients.
- **Pico's class build.** `pico.min.css` styles elements and classes such as `container`, `outline`, and `secondary`. Custom classes in `app.css` cover what Pico does not. Keep `app.css` small.
- **Teal on Pico, including dark mode.** Pico supplies type, spacing, layout, and components, and it follows the system color scheme. `static/theme.css` loads after Pico and is the only place that sets the brand, using Pico's color variables. Light mode uses deep teal `#0F766E` (hover `#115E59`), warm white `#FAFAF7`, white cards with warm inset fields, and slate text `#1E293B`. Mint `#5EEAD4` is a text selection and a dark-mode accent. It is not a fill behind white text. Dark mode keeps slate surfaces in three steps: the page, a lifted card, and a field set into the card. Links and focus use a lighter teal, and white text stays on the deep teal buttons. Secondary actions stay Pico's secondary color. Success, warning, error, and delete colors stay Pico's. The installed app icon is the teal tally, and the install theme color is `#0F766E`. A tracker may add one optional accent from a small built-in set of soft tints, on its card and icon only. The accent helps recognition and is never the only label. It has to stay clear in light and dark. Chrome icons use `currentColor` so they follow text color, including dark mode and button hover.
- **Obvious in a glance.** No decorative chrome. A tracker card is a name, an icon, a summary, and a button.

## Page layout

The site header stays on every page, including authentication. Page content sits in Pico's container, under that header, with ordinary spacing. Do not vertically center page content.

Dashboards, tracker history, and charts use the normal content width and align with the container. They are not squeezed into a form card. Home is a short stack of tracker cards in that width, not a calendar grid.

## Screens

Build the narrow screen first. The logging rules are in **Recording** below. Permissions are in `PLAN.md`.

### Home

Logged-in home is one list of every tracker the person can see. Each tracker is one card:

- The name, always visible.
- The chosen icon, or the tally-mark default.
- An optional accent, as a soft tint on the card or the icon. No accent means an ordinary Pico card.
- A summary line, from that tracker's summary display (`PLAN.md`). The log label does not choose this line.
  - **Times today** (the default): "Nothing logged today", "1 time today - Last: 6:42 AM", "3 times today - Last: 8:15 PM", or "None today". The time is the latest entry today, in the same clock as an entry. Nothing logged and none do not add it.
  - **Done today**: "Done today" when today has at least one entry, "Not done today" otherwise. A second entry today does not change the line.
  - **Last occurrence**: "Last: 6:42 AM" when the latest entry is today, "Last: Yesterday, 8:15 PM" when it was yesterday, "Last: Mon, Jan 2, 8:15 PM" when it was earlier, or "Never logged" when the tracker has no entries. After midnight, today's time becomes "Last: Yesterday, …". It does not become "Nothing logged today".
- One prominent logging button. On the Millie card that button is **+ Ate**. Logan Motrin can say **Gave Motrin**. The default label is **+ Log**. One tap records the current time and then offers **Undo**. The button text is the log label, not the summary. A number tracker shows a value field with that button even when nothing was logged today. The field is the last value, or empty when nothing has ever been logged. **Log again** is the button once a value exists; until then it is the log label. The field sits beside the button when the tracker is wide, and above the button on a smaller screen. A cumulative tracker shows the lifetime total, or progress toward the next repeating goal, in that summary line. With a goal it also shows a progress indicator, how many goals are complete, and the total earned. Its amount field starts empty. The button is **Add** plus the unit when the unit is a word, **Add amount** otherwise, or the custom log label when one is set.

Cards stack in a single column. They do not tile into a dense grid. On a wide screen they may sit in a quiet row, still with room around them and with the name and the button readable.

A tracker in the personal household shows no household name, even after that household is renamed. A tracker in any other household shows that household's name in small muted text under the tracker name. The name is a link to the household page. When two households on the page have the same name, after trimming surrounding whitespace and ignoring case, the one someone else created adds that person's email on the same line, so a partner's "My trackers" doesn't look like your own. Your own households never carry your email, and you cannot own two that match that way. "My trackers" is just the default name for a personal household. It does not suggest a shared family space.

There are no household headings and no separate containers. Home is one list, and its order is yours. A small drag handle sits on every card, including cards in a household you only belong to. Dragging it moves the card anywhere in the list, above or below cards from other households. It does not move the tracker to another household, and someone else's home does not follow your order. The handle is the only drag target, so the log button still works as a button. Until you drag, the list keeps your household order and then each household's tracker order. A tracker you have not placed yet, such as a new one, goes at the end. Home has no handle for moving a whole household.

When the list has no trackers, one line says there are none yet and links to creating one. An empty household does not add its own section.

Below the list, a quiet **New household** action (a `<details>` element, closed by default) opens one name field and a **Create** button. A name that matches another household you already own, after trimming surrounding whitespace and ignoring case, stays on the form with an error and creates nothing. Creating one lands on the new household's page, where the invite link is. Home does not turn into a household admin screen.

The header's global **New tracker** keeps its household picker. Starting from a household page is still the preferred path, because it opens the form already set to that household.

### Tracker detail

The header repeats the icon and the name, with the accent if there is one.

Order on a phone:

1. Today's logging control: the summary line, then the log button, then a small form with an optional note. The summary is the same line as on the card. Owners also see a time field, set to now, for backfill. Members don't see it. A number or duration tracker shows the carried value under the summary. **Log again** is the button once a value exists. On a number tracker the carried value is also in a field with that button: beside it when the tracker is wide, and above it on a smaller screen. Submitting logs that number. Before a value exists, a duration's button opens this page and the value form is open. A number's home card already has the value field, empty until something is logged. The note does not come along. Share links use the same carried value, without a note or a time. A cumulative tracker uses the same summary as its card: the lifetime total, or "8 / 32 points" with the progress indicator, the completed-goal count, and the total earned. The amount field starts empty. After an entry that finishes one or more goals, a short status line says "Goal completed!" or "2 goals completed!". A later visit without that result does not show it. The goal field on the edit form says that the goal repeats, that extra amounts carry forward, and that changing or clearing the goal recalculates from the full total without changing entries.
2. Today's entries. Each one can be undone while it is recent. An owner can edit the time or note in place, or delete with confirmation.
3. The trend. A count tracker shows a line of the last 30 local days once any of those days has a count or a recorded zero. A number or duration tracker shows one point per entry over that same window, once any entry in it has a value. Otherwise one sentence says nothing was logged in that window. The heading is Trend. A quiet control under the heading can mark one other count tracker's events on that chart.
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

Show the date, then that day's entries with the time they happened. A count's recorded zero reads as none. A day with no entries and no recorded zero reads as nothing logged. Those two states do not look the same. A number entry shows the value and the tracker's unit, such as "8:15 AM — 10 lb". A duration shows words, such as "8:15 AM — 42 min". A cumulative entry shows the amount and the lifetime total as of that entry, such as "8:15 AM — 10 points" and "40 points total". Number, duration, and cumulative history list the days that have entries. They do not offer Record none. The lifetime total on a cumulative tracker counts every entry. When the list leaves older entries out, a separate line names the sum of the entries it does show.

On a count tracker, an owner's unlogged history day offers two quiet actions: the tracker's log label, then **Record none**. The words match the logging button: **+ Log** by default, or a custom label such as **+ Ate**. The log records one event at noon on that day, so the row shows **12:00 PM**. A day that already has an entry keeps its time and **Edit**. A day recorded as none keeps **Clear**. Neither shows the log action. The owner can change the noon time with **Edit**.

Who logged an entry can be shown where it helps, without putting it on every line.

### Trends

Choose the chart from the tracker's data:

- Count: events per day, such as meals.
- Number: the value over time, such as workout weight.
- Duration: length over time.
- Cumulative: the lifetime running total. Finishing a goal does not pull the line back down.

A recorded zero is a zero. A day with nothing logged is a gap, not a zero.

The historical chart on the tracker page sits under the heading Trend. The count chart is a line of events per day for the last 30 local days, oldest at the left, including today. A recorded zero is a point at zero. A day with nothing logged is a gap in the line, not a zero. A number chart and a duration chart use that same window. Each entry is its own point, so two weights on one day stay two points. A cumulative chart uses that window too, and each point is the lifetime total earned through that entry. Amounts from before the window are included in the total, so the line does not start over. The duration axis is labeled in words, such as "45 min". If the window has nothing to plot, the page says "Nothing logged in the last 30 days." and does not draw a chart. Week and month views are not on the page. The charts page can look at a longer range of the same line. For a cumulative tracker that page still states the lifetime total. The range chooses which points are drawn. It does not replace the total.

One other tracker can be drawn on that same trend. The control sits with the Trend heading: **Show events from**, closed until opened, with **None** and each other count tracker the person can see. Each choice shows that tracker's icon and name. Choosing one reloads this page with `?overlay=`. Choosing **None** removes it. The choice is not saved.

An overlay day is a narrow translucent vertical band across the plot, in Pico's primary color at low opacity, so it stays visible in light and dark and stays quieter than the primary line. A short cap at the top of the band marks the day. The band is not a duration and not a second measurement. Several events on one local day are one band. Hover or tap names the overlay tracker and the local date, and lists the times when there are more than one. If the overlay has no events in the window, the primary chart stays and the page says "No Woods run events in the last 30 days." An overlay does not draw a chart when the primary tracker has nothing logged in the window. The page describes the bands and does not claim a cause. Woods outings on the meals chart are the reference. The same bands sit on a weight or duration chart.

### Charts

Charts is a separate screen for looking at one tracker. Home stays the logging page, and the tracker page keeps the Trend section above. The header link is the word Charts, for a logged-in user only.

The screen is three selectors and one chart:

- **Tracker.** Each tracker the person can see, with its icon and name. Choose one. Until then, one sentence asks for a tracker. Archived trackers and trackers they cannot see are not in the list.
- **Show events from.** None, or one other count tracker, with its icon and name. The tracker being charted is not in this list. Choosing one reloads the page with `overlay` in the query. None removes it.
- **Range.** Last 30 days, Last 90 days, Last 6 months, Last year. Last 30 days is the default. The choice is `range` in the query. There is no custom date field and no week or month bucket control.

The chart is the same Chart.js drawing as the tracker trend, and it is taller, because this page is for looking. A count is still one point per local day: a gap is nothing logged, and zero is a recorded none. A number or a duration is still one point per entry. Bands, tooltips, and the empty sentences match the trend, with the range named in the sentence ("No Woods run events in the last 90 days."). A tap or hover on a day shows that day's count and, when a band is there, the overlay name, date, and times. The page does not draw a line through the gaps, and it does not add a trend line, an average, or a claim about cause.

Nothing on this screen is saved. The address is the view. Copy it or reload it and the same chart comes back. There is no dashboard of cards and no saved layout.

On a phone the three selectors stack, and the chart uses the width of the page. On a wider screen the selectors sit in a row above the chart, and the chart uses the full content width, which is more room than the trend beside the log control. There is no separate desktop layout.

### Icon and accent

The create and edit forms offer a small picker. It contains a curated handful of clear Tabler icons — a paw, a pill, trees, a barbell — and a few emoji. It is not the whole Tabler set, not a free-text emoji field, and not an upload. The tally-mark icon is selected by default. The owner can change the icon or the accent later.

Show the icon on the home card, the tracker header, and every control that picks a tracker, including the overlay picker and the Charts selectors. The name stays beside it. Color and icon help recognition. They are not the only way to identify a tracker.

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

- The log button posts in place, updates the summary line immediately, and shows a short-lived **Undo**. While that request or an undo is in flight, the button is disabled and a spinning loader replaces its label. The loader is the button's text color, in light and dark. The label comes back if the request does not succeed. The server response confirms the summary. The update follows the summary display: a new Times today count with Last at the current time, "Done today", or "Last" at the current time. Undo disappears once the 15-minute window has passed. The default label is **+ Log**. A tracker can use a short custom label such as **+ Ate** or **Gave Motrin**. The tap still records the current time. Another tap on a Done today tracker records another entry.
- **Record none** is a quiet action on a count tracker, not a second primary button. It marks today as a recorded zero when today has no events. A Times today summary then says "None today", which is different from "Nothing logged today". Done today stays "Not done today". Last occurrence keeps the latest entry. Number and duration trackers do not offer it.
- Logging with a note, a time (owners), or a value adds the new row and leaves the form ready for the next one, prefilled from the latest value. The note does not carry forward onto the next entry, and the time goes back to now. A number is entered with the tracker's unit beside it. On a number tracker that field also sits with the button, on the home card as well as here: prefilled with the carried value, or empty when nothing carries forward. The home button is the log label until a value exists, then **Log again**. Submitting logs the number in the field and refreshes the last-value line. A bad number shows its message in that tracker. A duration is hours, minutes, and seconds. **Log again** on a duration skips the form and repeats the carried value. A cumulative amount is zero or positive, with the unit beside it when there is one. The field starts empty. The button is **Add** plus a word-like unit, **Add amount** otherwise, or the custom log label. Submitting adds that amount to the lifetime total. A negative amount shows its message in that tracker.
- Home, the tracker page, and the share page stay current on their own: every 30 seconds, and when the app comes back to the front, they quietly reload their content, so a partner's log shows up without a refresh. They never reload while someone is typing in a field or has a section such as **Log with a note** or an entry's **Edit** open. There is no spinner or banner for it.
- Controls a person can't use aren't shown. Members get the note field but no time field. Share-link visitors get the summary line, the log button, **Undo**, and **Record none** on a count tracker. On a number or duration tracker they get **Log again** when a value exists, and a value form with no note and no time. The summary is the same line as on the card.
- Owners edit an entry's time, note, and, for a number or duration, its value in place. Delete asks for confirmation. Those controls sit on the entry. Members and share links still only undo while the entry is recent.
- **Download CSV** is a quiet owner link under history. It is that tracker's entries, plus recorded zeros on a count tracker.
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

Logged out: **Log in** and **Sign up** in the header. Logged in: **Charts**, settings, and log out. Charts is one text link. The brand mark links home, which stays the logging page. Icon-only controls in the header need an accessible name (settings, log out).

## Icons

[Tabler Icons](https://tabler.io/icons) (MIT). Use an icon only where it helps comprehension; a text label is fine. Icon-only buttons carry an accessible name. Decorative icons are hidden from assistive tech. How they are embedded is in `ARCHITECTURE.md`.

Tabler instead of Font Awesome: an icon font or a JavaScript loader is the wrong trade here, and Font Awesome's CC BY license was a worse fit.

Chrome icons, the ones the app itself uses: `tallymarks` (the default tracker icon), `plus`, `arrow-back-up` (undo), `loader` (log and undo while the request is in flight), `trash`, `pencil`, `chart-line`, `settings`, `share`, `users`, `archive`, `calendar`, `link`, `logout`, `grip-vertical` (reorder). Form labels add `mail` (email), `lock` (password), `lock-check` (confirm password), and `timezone`.

The header brand mark is the same teal tally as the favicon, Apple touch icon, and home-screen icons. It sits beside the name and links home. The other chrome icons stay the Tabler set above.

The tracker picker is a separate handful, described under **Screens**. Copy in a Tabler SVG when it joins that picker. Emoji in the picker are text, not SVGs. Do not offer the whole Tabler set, and do not accept an uploaded icon in the first version.

## Ads

When ads exist, they stay out of share, login, and settings pages, and out of the view for anyone on the ad-free plan (including the grace period). They must not reappear or reload when part of the page updates in place.

## Responsive behavior

Design the narrow screen first. There is still one web layout and no native app. On a phone, the logged-out home and the dashboard show a short offer to add the app to the device home screen. It can be dismissed. The manifest is specified in `ARCHITECTURE.md`. Offline logging, later, keeps this same log button.

On a phone, tracker cards stack, and the logging control is the first content on a tracker page. The trend follows, then history, so reviewing is a scroll down the same page. On Charts, the selectors stack and the chart fills the width. A tap on a day shows the count and any overlay for that day.

On a wider screen, focused forms stay narrow and centered. A tracker page may place the trend and history beside the logging column, with the trend above history, inside the normal content width. A chart does not take the place of the log control. On Charts the selectors can sit in one row, and the chart uses the full content width, taller than the tracker-page trend.

Forms and cards use the width they have, with the page margins Pico already applies.
