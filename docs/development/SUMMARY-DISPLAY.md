# Summary display — implementation plan

How the tracker setting in `PLAN.md` (Summary display), `DESIGN.md` (Home, Tracker detail, Focused forms, Recording), and `ARCHITECTURE.md` (`Tracker.SummaryDisplay`, the create/edit routes, and the Summary display test bullet) was built. Those documents decide the behavior. This one says where the code lives.

**Done.** The order of work below is in the app, and `make test` covers the cases under Tests. The sections from Data through Optimistic update describe that finished change.

This is a presentation setting on an existing count tracker. Do not add a tracker kind, a new table, or a limit of one entry per day.

## Outcome

An owner chooses Times today, Done today, or Last occurrence on create and on edit. The sentence above the log button follows that choice on the home card, the tracker page, and the share page. History rows, recorded-zero actions, and the empty today-list stay as they are. Existing trackers, and a create that omits the field, stay Times today, so a phase 2 card still reads "3 times today".

**Done when** matches `PLAN.md`: Millie ate reads "2 times today", McGill Big 3 reads "Done today" after one log and still after a second log the same day, and Logan Motrin reads "Last: 6:42 AM" on that day and "Last: Yesterday, 8:15 PM" the next day. `make test` passes, including the cases under Tests.

## Where the line is built

`summaryLine` in `days.go` chooses the sentence. `todaySummary` stays the Times today wording. `cardFor` in `handlers_trackers.go` calls `summaryLine` for every card. The same `logControl` partial renders that sentence on the home card, the tracker page, and the share page (`templates/partials/tracker.html`).

Three callers build a card, and all three go through `cardView`:

- `app.card`, used by the dashboard in `handlers.go`
- `trackerPage` in `handlers_trackers.go`, which calls `cardView` after `loadToday`
- `renderShare` in `handlers_share.go`, which calls `cardView` the same way

`loadToday` only loads the current local day. That is enough for Times today and Done today. Last occurrence loads the entry with the latest `OccurredAt` through `latestEntry`, including entries from earlier days. `cardView` calls `latestEntry` only when the display is `last`.

`static/app.js` rewrites the sentence on a quick-log tap from `data-summary-display`: the next Times today count, "Done today", or "Last:" at the current time in `data-time-zone`. The server's redirect replaces it.

Create and edit go through `readTrackerForm`, `handleCreateTracker`, and `handleUpdateTracker`, and `templates/tracker_form.html`. The form has name, icon, accent, log label, and summary display.

Undo, delete, and editing an entry's time redirect back and rebuild the page. They do not have their own summary code. The line is computed at render.

## Data

Add one column to `Tracker` in `models.go`, matching the struct in `ARCHITECTURE.md`:

```go
SummaryDisplay string `gorm:"not null;default:times"` // "times", "done", or "last". Empty means times.
```

`db.go` already `AutoMigrate`s `Tracker`. No one-time backfill. SQLite fills existing rows with `times` when the column is added. `Kind` stays `"count"`. Entries and recorded zeros are unchanged.

Stored values are `times`, `done`, and `last`. Empty means `times` on read and on write. A missing form field is empty, so current create requests that do not send the field keep today's behavior. `boolean` is not a stored value.

Constants next to `defaultLogLabel`:

```go
summaryTimes = "times"
summaryDone  = "done"
summaryLast  = "last"
```

A `[]choice` for the form, same shape as `trackerAccents`:

| Value | Label |
| --- | --- |
| `times` | Times today — show how many times it happened today |
| `done` | Done today — show whether it happened today |
| `last` | Last occurrence — show when it last happened |

`listed` already checks a `[]choice`. Use it. Do not accept any other string.

## The sentence

Add a pure function in `days.go`, beside `todaySummary`. `todaySummary` stays the Times today implementation. `dayCount.Summary` stays the history row ("1 time", "None", "Nothing logged") and is not called for this setting.

```go
// summaryLine is the sentence above the log button.
// latest is nil when the tracker has no entries. zero is ignored unless display is times.
func summaryLine(display string, count int, zero bool, latest *time.Time, loc *time.Location, now time.Time) string
```

Empty `display` is Times today.

**Times today.** `todaySummary(count, zero)`: "Nothing logged today", "1 time today", "3 times today", or "None today".

**Done today.** `count > 0` is "Done today". Otherwise "Not done today", including a recorded zero and including entries that exist only on other days. A second entry is still stored; the sentence stays "Done today". `latest` is unused. This display does not make the tracker or its entries a yes-or-no type.

**Last occurrence.** Ignore `count` and `zero`.

- `latest == nil`: "Never logged".
- The entry's local day is today: "Last: 6:42 AM". Clock is `3:04 PM`, the same layout as `entryViews`.
- The entry's local day is the previous local calendar day: "Last: Yesterday, 8:15 PM".
- Older: "Last: Mon, Jan 2, 8:15 PM". Date is `Mon, Jan 2`, the same layout as a history label, then the clock.

"Today" and "yesterday" use `localDay` and `AddDate(0, 0, -1)` on the local date, the same way `countsByDay` walks days. Do not subtract 24 hours. A spring-forward or fall-back day is still one calendar day. The zone is the `*time.Location` the caller already uses: the viewer's stored zone, or the household first owner's zone on a share page.

The latest entry is `occurred_at DESC, id DESC`, the same order as `loadToday`, limited to one row for the tracker. A backfilled entry wins only when its `OccurredAt` is later than the others. Equal timestamps show the same clock.

## Rendering the line

`cardFor` cannot see older entries. Give it the latest entry (nil unless the display is `last`):

```go
func cardFor(t Tracker, s todayState, latest *Entry, now time.Time, loc *time.Location, viewerID *uint) trackerCard
```

`Summary` becomes `summaryLine(t.SummaryDisplay, s.Count, s.Zero != nil, occurred, loc, now)`.

Load that row in one helper used by every card path:

```go
func (a *app) latestEntry(trackerID uint) (*Entry, error)
```

Call it only when `SummaryDisplay` is `last`. Times today and Done today keep the single `loadToday` query they already have. `app.card`, `trackerPage`, and `renderShare` each have a location and a `loadToday` result; each passes both into `cardFor`.

Put two attributes on the summary element in `logControl`, next to `data-count`:

- `data-summary-display`: `times`, `done`, or `last`. Render empty as `times`.
- `data-time-zone`: the IANA name of the `*time.Location` used for the page (`loc.String()`).

`trackerCard` needs the zone string so the partial can print it. Home, the tracker page, and the share page all use that partial, so one template change covers all three.

Do not change `templates/tracker_show.html` history (`day-summary`) or the today-list empty copy ("Nothing logged yet" / "Recorded as none."). "Never logged" is only the Last occurrence summary. Record none, undo none, and the share-page "Record none for today" control stay on every count tracker.

## Create and edit

Add `SummaryDisplay` to `trackerForm`. `readTrackerForm` reads `summary_display`.

- Missing or empty stores `times`.
- `times`, `done`, or `last` stores that value.
- Anything else, including `boolean`, returns a validation error ("Pick a summary from the list.") and writes nothing. Create re-renders the form. Update does not call `Updates`.

`handleCreateTracker` sets the field on the new `Tracker`. `handleUpdateTracker` adds `summary_display` to the `Updates` map, beside `log_label`. Leave `log_label` on its own key so saving one does not overwrite the other. `editForm` copies the stored value onto the form. Empty stored value selects Times today.

In `templates/tracker_form.html`, put a fieldset after the button label and before the submit button. Legend: "Summary display". Three ordinary radios named `summary_display`, each label the choice text from the table above:

- Times today — show how many times it happened today
- Done today — show whether it happened today
- Last occurrence — show when it last happened

Times today is checked when the form value is empty or `times`.

Do not use the `picker` class. That class hides the radio and expects an icon or a swatch. These are ordinary labeled radios. Pico already stacks them. No new CSS unless a real screen shows the legend and the option text colliding.

Create from the household page (`/trackers/new?household=`) only pre-selects the household. It does not pre-select a summary. Times today stays the default there too.

Members never see this form. Create and edit are already owner-only. Do not add a route.

## Optimistic update

In `static/app.js`, the quick-log listener branches on `data-summary-display` before it touches the text:

- `times`, or a missing attribute: keep the current count increment and "1 time today" / "N times today".
- `done`: set the text to "Done today". Leave the entry creation to the server. A second tap the same day still posts; the line stays "Done today".
- `last`: set the text to "Last: " plus the current time in `data-time-zone`, formatted as `h:mm AM` with a normal space (6:42 AM, not a browser-local zone and not a narrow no-break space). Use `Intl.DateTimeFormat` with that IANA name, `hour: "numeric"`, `minute: "2-digit"`, `hour12: true`, then join the hour, minute, and day-period parts so the space matches Go's `3:04 PM`.

The server response still replaces the sentence. Undo and delete do not need an optimistic branch; their handlers already redirect and the new page is authoritative.

Do not read the browser zone. Signup already stored the account zone, and a share page is using the household owner's zone. The attribute is that zone.

## Out of scope

- New values of `Kind`. No medication, habit, or feeding tracker. Done today is a summary line, not a kind, and it does not change what an entry stores.
- Rejecting a second entry on a Done today tracker. Both rows are stored.
- Changing history, the 30-day list, or future charts to Done / Last.
- Hiding Record none on Done today or Last occurrence trackers.
- A fourth summary, streaks, period progress ("2 / 3 this week"), or "3 days ago". Period progress belongs to the later recurring-tracker direction in `PLAN.md`.
- Number and duration trackers (phase 4). When those arrive, this setting still only describes count entries until someone extends it on purpose.
- Offline logging. The optimistic line in `app.js` is still replaced by the server response. Queuing a tap while offline is a later enhancement in `PLAN.md` and `ARCHITECTURE.md`.

## Tests

These cases are in the suite. Pure cases are in `days_test.go`, table-driven, no HTTP. Handler cases are in `trackers_test.go` and `share_test.go`, using the cookie jar and the fixed clock the suite already has.

**Wording** (`summaryLine`):

- Times today: the four strings `TestTodaySummary` already locks ("Nothing logged today", "None today", "1 time today", "3 times today").
- Done today: count 0 with and without a recorded zero, both "Not done today". Count 1 and count 4, both "Done today". A `latest` timestamp on another day does not matter when count is 0.
- Last, no entry: "Never logged". A recorded zero and no entry is still "Never logged".
- Last, same local day: "Last: " plus `Format("3:04 PM")`.
- Last, previous local day: "Last: Yesterday, " plus the clock. Build "yesterday" with `AddDate(0, 0, -1)` in `America/Los_Angeles`.
- Last, the spring-forward boundary already used in `TestDayBoundsAcrossDST`: an entry at 2026-03-07 23:30 America/Los_Angeles, `now` on 2026-03-08, reads "Last: Yesterday, 11:30 PM", not "Nothing logged today".
- Last, older than yesterday: "Last: Mon, Jan 2, 3:04 PM" for a fixed instant, compared to `Format("Mon, Jan 2, 3:04 PM")`.
- Last, two instants: the later `OccurredAt` wins even when it was written second in the table (the function takes one `*time.Time`; the handler test below checks the query order).
- Empty display matches Times today.

**Create and edit:**

- Omit `summary_display`. The row stores `times` (or empty, which renders as Times today). The card still says "Nothing logged today". Existing create tests that do not send the field must keep passing.
- Post `done` with log label "+ Ate", then post again with a different label and without changing `summary_display`, and the reverse. Each save changes only the field that was sent. Assert both columns.
- Post `last` with label "Gave Motrin", icon 💊, accent `green`. The edit form shows Last occurrence selected and the label still "Gave Motrin".
- Post `summary_display=habit` and `summary_display=boolean`. Each is status 422, the error is shown, and the stored display is unchanged.
- A member's update still changes nothing. The existing owner-only test covers the route; assert `SummaryDisplay` is untouched if that test already reloads the row.

**Pages**, one tracker each, viewer's zone America/Los_Angeles:

- Times today, two entries today: home, tracker page, and share page all contain "2 times today". History for a previous day still contains the day-summary "None" or "Nothing logged", not "Done today".
- Done today, one entry today: "Done today". A second quick-log the same day adds a second `Entry` and the line stays "Done today". Undo the only entry today: "Not done today". Mark today none with no entries: the summary is "Not done today", and history can still say none.
- Done today, entries only on an earlier day: "Not done today".
- Last, never logged: "Never logged" on the summary element. The today-list may still say "Nothing logged yet"; that is the list, and this test should target the summary paragraph (`tracker-summary`).
- Last, an entry at 6:42 AM today: "Last: 6:42 AM". Move the clock to the next local day without adding an entry: "Last: Yesterday, 6:42 AM", and the body does not contain "Nothing logged today" as the summary.
- Last, an entry older than yesterday: the `Mon, Jan 2` form.
- Last, an entry today and an owner backfill of an earlier day: the line stays on today's time. Owner edits the latest entry's time to yesterday: the line becomes "Last: Yesterday, …".
- Last, today marked none, plus an entry yesterday: the line is yesterday's "Last: Yesterday, …".
- Share page for a Done today tracker and a Last occurrence tracker shows the same sentence as the owner's card. The share page's zone is the household first owner's zone, which the share tests already fix.

**Regression.** `TestTodaySummary` and `TestCountsByDay` stay. A tracker created the way phase 2 tests create one still renders "Nothing logged today", "1 time today", and "None today".

## Order of work (done)

1. Column, constants, and `summaryLine`, with the `days_test.go` table.
2. `latestEntry`, `cardView`, `cardFor`, and the three call sites, with the Done today and Last occurrence page tests.
3. Form read, create, update, and the template, with the validation and independence tests.
4. `data-summary-display`, `data-time-zone`, and the `app.js` branch.

All four are in the app. Omitted and empty mean `times`, so existing Times today tests stay on that wording.
