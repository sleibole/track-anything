# Track Anything — Product

A tiny, general-purpose tracking app at **trackanything.io**, for things currently annoying enough to record on paper, in Notes, or in an ad-hoc spreadsheet. Creating a tracker and recording an observation should be almost frictionless; the accumulated history then turns into useful counts and simple charts.

The first real use: counting how many times the family dog eats each day, replacing tick marks on paper. Either person in the house can log a meal.

## Project documentation

Track Anything documentation is divided into two categories:

- `docs/development/` contains internal product, design, and architecture decisions used while building Track Anything.
- `docs/help/` contains user-facing help documentation, written in Markdown and rendered as HTML by the app. Nothing lives there yet. Do not put developer notes in `docs/help/`, and do not put user help in `docs/development/`.

Within `docs/development/`:

- `PLAN.md` describes what Track Anything is and what we're building.
- `DESIGN.md` defines visual design and UI/UX conventions.
- `ARCHITECTURE.md` records technical and implementation decisions.
- `SUMMARY-DISPLAY.md` records how the summary display setting was built. The behavior itself is in the three documents above.

These development documents contain settled project decisions and should be followed when implementing new features.

## North-star test

**Would we actually use Track Anything instead of the scrap of paper?**

If the answer is yes for dog meals, yes for recording a woods outing, and eventually yes for a simple workout measurement, the product is doing its job. Every feature should make tracking easier or make the accumulated history more useful.

## Product principles

- **Dogfood immediately.** The first version solves a real household problem from day one.
- **Tracking must be faster than thinking.** Common events are recordable with one or very few taps.
- **Generic underneath, concrete in the UI.** The data model supports many uses without making anyone understand a schema builder.
- **History becomes more valuable over time.** A simple chart of what was tracked, then a related event drawn on that chart, is the payoff for recording. Richer summaries can wait. The person looking at the chart notices the pattern.
- **Shared by default where it helps.** A tracker can be used by a whole household, or by anyone with its link, without everyone needing an account.
- **Micro-product economics.** Operating cost and complexity stay low enough that a $5/year product is viable.
- **Product is the point.** Use AI aggressively to deliver the product. Hand-building small tutorial projects is still valuable when learning Go is the goal, but Track Anything is about shipping.

## Guardrails (non-goals)

- Do not become a full fitness platform.
- Do not become a medical diagnostic product, or an analytics product that computes causes. Charts are for looking. A pattern is not a diagnosis.
- Do not add a frontend framework or a product-analytics stack in order to draw charts. Chart.js on the existing pages is enough (`ARCHITECTURE.md`). A Prometheus scrape for the server is a later idea in `ARCHITECTURE.md`. It is not a charting tool, and it is not in the app.
- Do not become a habit-coaching or gamification platform unless real users clearly demand it. A later idea may attach an optional schedule to an ordinary tracker. That idea does not make Track Anything a calendar, a todo list, or a habit tracker, and it is not part of the phases below. See Later ideas.
- Do not add a tracker kind to answer a different question about the same events. How many times, whether it happened today, and when it last happened are a summary display on an ordinary tracker. See Summary display.
- Do not sacrifice the one-tap tracking experience for generic configurability.
- No native mobile apps. Home-screen install is a web app manifest, not a store app.
- No fully offline application. Through phase 5, logging talks to the server. A later enhancement covers the primary log action only, for trackers the device has already loaded. See Later ideas.
- No public API.
- Keep the implementation small. The engineering limits (no microservices, no schema language, no scale work before there are users) are in `ARCHITECTURE.md`.

## Reference use cases

**Dog eating and GI trouble.** Log each time the dog eats. Some days she eats three or four times; occasionally she eats very little or not at all, and she may have stomach noises or diarrhea. Those days may follow off-leash time in the woods, where she could eat something she found. A second tracker records a woods outing. Marking that outing on the meals-per-day chart makes the one to three days afterward easy to inspect. The inputs stay simple. How the chart works is under Charts and overlays. The dog is the test case for overlays in general, not a separate feature.

**Workout progression.** Movements, load, duration, or reps over time, to see progression in club, kettlebell, and related training (e.g. Mark Wildman's TOI program, McGill's Big 3) without turning Track Anything into a fitness app. "Did I do McGill today?" is not this. That question is the Done today summary display on one ordinary tracker, under Summary display.

## Tracking model

Start with the smallest useful tracker and add value shapes only when a real use case needs them.

| Kind | Each entry records | Examples | Arrives in |
| --- | --- | --- | --- |
| Count | A timestamp | Dog ate, medication taken, symptom occurred | Phase 2 |
| Number | A timestamp and a number with the tracker's unit | Body weight, kettlebell weight, temperature | Phase 4 (built) |
| Duration | A timestamp and a length of time | Off-leash time, workout length | Phase 4 (built) |

- Any entry can have an optional **note**, but the note never slows down the normal one-tap path.
- Each tracker has an optional **icon** and an optional **accent**. The icon comes from a small built-in picker of clear icons or emoji and defaults to the tally-mark icon. The accent comes from a small built-in set of soft colors and defaults to none. Both can be changed later. The name is always shown with them. Someone picking a tracker can tell it by the name alone. User-uploaded icons are a later idea, not part of the first version.
- The card's logging button uses a short **log label**. The default is "+ Log". It can be changed to a few words such as "+ Ate" or "Gave Motrin". The tap still records the current time. The log label and the summary display are separate settings. Changing one leaves the other as it was.
- Each tracker has a **summary display**: Times today, Done today, or Last occurrence. It only changes the summary line above the log button. The tracker is still a list of timestamped entries. Details, including the three wordings, are under Summary display. The default is Times today, which is what phase 2 shows. The setting itself arrived in the small follow-on after phase 2.
- A tracker has **no schedule** in these phases. Recurrence, a period goal, and a reminder are a later direction: optional metadata on an ordinary tracker, not a new kind. See Later ideas.
- A count tracker can store one **recorded zero** for a calendar day: the count is deliberately none. That mark is not an event and does not increase the count. A day with no entries and no recorded zero is **nothing logged**. Logging an event for that day clears the recorded zero. Charts and per-day counts draw a recorded zero as zero and leave an unlogged day blank. A recorded zero is not an entry, so Done today and Last occurrence ignore it when they build the summary line. History still shows it.
- For number and duration trackers, **the last value carries forward**: if you used a 5lb club last time, the form already says 5, and **Log again** records it with one tap. A duration works the same way, shown as words such as "45 min" or "1 hr 10 min". Backfilling an older entry does not change what carries forward. The note does not carry forward, and the time defaults to now. Changing the prefilled value (moving up to 6lb) and submitting is how a new value starts carrying forward. The button says **Log again**. The custom log label stays on a count tracker, and on the link that opens a number or duration tracker before it has a value.
- A tracker's kind can change until it has an entry or a recorded zero. After that it stays, so old rows are not reinterpreted. The unit on a number tracker can still be edited. There is no unit conversion.
- Members log at the current time, with an optional note. Owners can also choose when an entry happened, which is how they backfill ("I forgot to log yesterday"). When it happened is separate from when the row was written. Undo uses when it was written. Marking an earlier day as none is the same kind of backfill: owner only.
- **Archive, not delete**, for trackers. Archiving hides the tracker and is the normal way to say "I don't want this tracker anymore." It is reversible: the owner restores it from the household page. Permanently deleting a tracker and its entries is a separate, confirmed owner action that arrives in phase 5, with the other destructive privacy operations.
- **Fields and sub-trackers** (one workout containing several exercises, each with weight, reps, and sets) are designed below but deferred. They get built only if number and duration trackers prove painful for workouts in real use.

## Sharing

Trackers belong to a **household**, and a single tracker can also be shared by **link**.

**Households.** A household is the unit of sharing. Everyone in it sees and logs all of its trackers. The owner can invite people with an **invite link** (copied and sent by hand; the app sends no invite email). Opening it asks you to log in or sign up, then adds you as a member. You can belong to several households and own several. The dashboard shows trackers from all of them, grouped by household.

**Personal household.** Every user gets one at signup, named "My trackers", with that user as owner. It is the default place for that person's private trackers. It is an ordinary household: the same model, the same rules, and no separate personal-tracker type. It is not shared with a spouse or family by default. Joining someone else's household does not expose either person's personal household. The name is only a default and can be renamed.

**Shared households.** Trackers meant for more than one person live in a separate household that someone creates for them, such as Family, Home, or Millie. Dogfooding phase 2 showed why. Inviting a spouse into "My trackers" shares every tracker in it, and some trackers should stay private. A tracker like "Girl stuff" stays in one person's personal household, while Millie ate and Woods run belong in a shared Family household:

```
Sheldon                         Wife
├── My trackers                 ├── My trackers
│   └── private trackers        │   └── private trackers
└── Family                      └── Family
    ├── Millie ate                  ├── Millie ate
    └── Woods run                   └── Woods run
```

Family is one household that both people belong to. Each person's "My trackers" is their own. Creating households, renaming them, moving a tracker between households you own, and leaving a household arrived in the Household organization follow-on after phase 2. Phase 2 itself left them out on purpose. The sharing model does not change: a tracker belongs to exactly one household, and membership in that household decides who can see it.

Household names do not have to be unique across people. You cannot own two whose names match after trimming surrounding whitespace and ignoring case, so "Family" and " family " cannot both be yours. Another person can still own "Family", and belonging to their Family does not stop you owning your own. When two households you can see share a name under that same comparison, such as your "My trackers" and a partner's, the one someone else created shows that person's email beside its name. Otherwise the name stands alone.

**Share links.** The owner can turn on a share link for one tracker. Anyone with the link can use that tracker **without logging in**. That covers a kitchen tablet, a dog sitter, or a relative who will never make an account.

**What each person can do:**

| Action | Owner | Household member | Anyone with the share link |
| --- | --- | --- | --- |
| See the tracker and its recent entries | Yes | Yes | Yes (that one tracker only) |
| Log an entry at the current time | Yes | Yes | Yes (log button only) |
| Add an optional note when logging | Yes | Yes | No |
| Undo a recent entry | Yes | Yes | Yes |
| Mark today as none, or undo that mark within 15 minutes | Yes | Yes | Yes |
| Choose when a new entry happened (backfill) | Yes | No | No |
| Mark an earlier day as none, or clear an older mark | Yes | No | No |
| Edit an entry's time or note, delete older entries | Yes | No | No |
| Create, edit, archive, or restore trackers | Yes | No | No |
| Turn on, regenerate, or turn off a share link | Yes | No | No |
| Invite members, remove members, promote a member to owner, regenerate the invite link | Yes | No | No |
| Rename the household | Yes | No | No |
| Move a tracker to another household they own | Yes | No | No |
| Leave the household | No | Yes | No |

Any logged-in user can create a new household and becomes its owner.

- **Recent** means an entry or recorded zero created in the last 15 minutes, by when it was saved, not when it happened. That covers "oops, I tapped twice" without letting a link holder wipe history. The window is a constant, easy to change.
- The server enforces the time rule: a member's entry always happens now, even if a request supplies a different time. The later offline sync accepts the time of that tap, and only on that path. See Later ideas.
- A household can have more than one owner, so ownership can be shared.
- Ownership management stays small on purpose. Owners invite, remove members, and promote a member to owner. Removing applies to members only: an owner cannot remove themselves or another owner. There is no demotion. A regular member can leave a household; an owner cannot use that route. Owner departure, ownership transfer, sole-owner cases, and deleting a household are not designed yet. They belong with phase 5 account deletion and the other destructive operations.
- **Regenerating** a share link or invite link makes the old one stop working immediately. Turning a share link off does the same. Archiving a tracker turns its share link off, and restoring it leaves sharing off until the owner turns on a new link.
- Members who want their own trackers make them in their personal household, where they are the owner. Private trackers stay there. Shared trackers go in a shared household.
- Entries remember who logged them, or that they came in through a share link. The UI can show that where it helps without cluttering every view.
- A tracker you can't see is indistinguishable from one that doesn't exist.

## Accounts and login

Two ways in, same account. The screens lead with email. A password is optional and secondary. See `DESIGN.md` for how that is presented.

- **Magic link.** Enter your email, get a one-time link valid for 15 minutes. Opening it shows a **Log in as you@example.com** button, and pressing that logs you in. The extra press is deliberate: email security scanners open links before the person does, and a link that logged in on open would be used up by the scanner. The same mechanism handles "forgot password": log in with a link, then set a new password in settings. Changing the password logs out every other session.
- **Email and password.** Password is optional. It is set in settings after the email is confirmed, not at signup. Password login does not work before that confirmation.
- Requesting a link shows the same "check your email" page whether or not the account exists, so the form can't be used to find out who has an account. The same page is shown when that address has been sent too many of these emails.

Signing up needs only an email and starts a session in that browser. It does not store a password. A confirmation link is emailed. The first time the mailbox is proven, by that link or by a magic link, any earlier session is signed out and any password set before the confirmation is removed. The confirmation email tells the recipient to open the link even if they did not sign up. Magic links for an address that is not confirmed yet do the same. How this is stored is in `ARCHITECTURE.md`.

Sessions last 30 days. Once a session is past half its lifetime, the next request renews it for another 30 days, so active users stay logged in.

## Calendar days

"Today" and which day an entry belongs to follow a time zone captured when the account is created. Traveling does not move entries onto different days. Signup does not ask for a zone. If it is wrong, settings offers a low-profile friendly name, not a raw zone identifier. People on a share link have no zone of their own, so a household shares one "today". The storage and detection rules are in `ARCHITECTURE.md`.

## Interface

A calm, mobile-first tracking notebook. Logging is fast, review is easy, and the same screens cover the dog's meals and a workout measurement. Tone takes a little from a family calendar: a recognizable icon, a soft color, an approachable card. The layout stays sparser than a calendar. How the screens look is in `DESIGN.md`.

**Home.** Trackers stay grouped by household. Each one is a card with its name, icon, optional accent, a summary, and one logging button. The summary follows that tracker's summary display. Millie ate, set to Times today, reads "2 times today". McGill Big 3, set to Done today, reads "Done today". Logan Motrin, set to Last occurrence, reads "Last: 6:42 AM". The button records the current time in one tap, then offers **Undo**. The button's words are the log label, which is separate from the summary. Before that setting existed, phase 2 showed the Times today wording on every card.

**Tracker detail.** Today's logging control and today's entries come first. The trend follows, then history. Correcting a mistake sits on the entry. Anyone who can log can undo an entry or a recorded zero from the last 15 minutes. Only an owner can choose when a new entry happened, edit the time, note, or value, delete an older entry, mark an earlier day as none, or clear an older recorded zero. On a phone the logging action stays first. On a wider screen, the trend and history can sit beside that column, with the trend above history. They never sit above the log control.

**History.** Dated entries, with the time they happened. "None" (a recorded zero) and "Nothing logged" are different rows. That distinction matters for the dog: a day with no meals is data, and a day nobody wrote down is a gap.

**Trends.** The chart matches the tracker: events per day, a value over time, or a duration over time. A recorded zero is a zero. A day with nothing logged is a gap. Each chart is on this page: the last 30 local days, including today. Week and month views are not built. One other count tracker's events can be drawn on that chart as shaded bands. See Charts and overlays.

**First slice.** Phase 2 replaces paper tick marks: create a tracker, choose an icon, log an event with the current time, correct a mistake, and review recent history, including a recorded zero versus nothing logged. An accent and a custom log label can be set then too; both are optional. Summary display was the small follow-on after that slice was in real use. Richer workout inputs stay in phase 4. Phase 3 is the historical chart, then overlays of related events. The daily count chart is on the tracker page, and one other tracker's events can be marked on it. Event-relative summaries are a later idea.

## Charts and overlays

Charts stay an extension of tracking. The sequence is:

1. Store simple observations and events.
2. Show a straightforward historical chart of one tracker.
3. Draw a related event on that chart.
4. Let the person looking at it notice the pattern.
5. Later, once there is enough history, summarize another tracker on the days before and after a repeated event.

That last step is the event-relative view under Later ideas. It is not part of the first charts, and it is not a statistics or correlation feature.

Chart.js draws the charts. It fits the server-rendered pages and HTMX, so visualization does not need a frontend framework. How it is loaded is in `ARCHITECTURE.md`. Layout is in `DESIGN.md`.

Charts stay secondary to recording. The log control comes first, a trend never sits above it, and logging works if the chart does not load. The historical chart lives on the tracker page, below the log control or beside it on a wide screen. An overlay is drawn on that same chart. There is no separate charts page.

- **Historical chart.** One tracker over time. Counts are events per day (meals per day). Numbers are a value over time (workout weight), one point per entry, not a daily sum. Durations are length over time, the same way. A recorded zero is drawn as zero on a count chart. A day with nothing logged is left blank on a count chart. All three use the last 30 local days, including today, on the tracker page. Week and month buckets are not built.
- **Overlay.** Events from one other tracker you can see, drawn on that historical chart. The treatment is a narrow translucent vertical band on each local day that has an event. The band marks the day. It is not a duration and not a second count. Several events on the same local day are one band. The picker shows the icon and the name, and None removes the overlay. The choice is a query on the tracker page (`?overlay=`). It is not stored. Dog eating and woods outings, in Reference use cases, are the test case: you look at the one to three days after an outing. The page describes what is on the chart and never claims a cause.
- The overlay is general. The same chart could later mark a workout against later soreness or recovery, alcohol against sleep, coffee against anxiety or energy, a medication against symptom frequency, or a late bedtime against next-day energy.

## Summary display

A presentation setting on the tracker, discovered by using the phase 2 app. It chooses which question the summary line answers. It does not create a tracker kind, and it does not change an entry. There is no medication tracker, habit tracker, or feeding tracker. Done today does not make the tracker or its entries a yes-or-no type. Millie ate, McGill Big 3, and Logan Motrin are three ordinary trackers with three different summary displays.

```
tracker
  -> timestamped entries
```

The owner sets it on create and on edit, independently of the name, icon, accent, and log label. One worked example:

- Name: Logan Motrin
- Icon: 💊
- Accent: green
- Log label: Gave Motrin
- Summary display: Last occurrence

The summary line is the sentence above the log button. It appears on the home card, on the tracker page, and on the share page. History, the last-30-days list, and later charts stay the per-day record: a count, a recorded zero, or a gap. They do not follow this setting. The empty today-list ("Nothing logged yet" when today has no rows) is that list, not this summary.

The page's zone is the viewer's stored zone, or the household first owner's zone on a share page. The same zone already decides "today". The clock uses the same `3:04 PM` form as an entry time. A date older than yesterday uses the same `Mon, Jan 2` form as history.

**Times today.** How many times did this happen today? Stored as `times`. This is the phase 2 wording, and the default for a new tracker and for every tracker that already exists.

- No entries and no recorded zero today: "Nothing logged today".
- One entry today: "1 time today".
- More than one: "3 times today", using the count.
- A recorded zero and no entries today: "None today".

Entries on other days are not part of this count. Millie ate uses Times today and reads "2 times today".

**Done today.** Did this happen today? Stored as `done`. The summary is whether today has at least one entry. The name is the summary line's question. It is not a kind of tracker, and it does not change the entries.

- At least one entry today: "Done today".
- No entry today: "Not done today".

"Not done today" covers a tracker that has never been logged and a tracker that was logged on some other day. A second entry today is stored normally, with its own timestamp, and the summary stays "Done today". Done today does not limit the tracker to one entry per day. A recorded zero is not an entry, so the summary stays "Not done today" when today is marked none. McGill Big 3 uses Done today and reads "Done today".

**Last occurrence.** When did this last happen? Stored as `last`. The summary is the entry with the latest `OccurredAt`. It stays that entry after midnight. A new day does not replace it with "Nothing logged today".

- No entries on the tracker: "Never logged".
- The latest entry is today: "Last: 6:42 AM".
- The latest entry is the previous local calendar day: "Last: Yesterday, 8:15 PM".
- The latest entry is older than yesterday: "Last: Mon, Jan 2, 8:15 PM".

"Yesterday" is the previous local calendar day, including across a daylight-saving change. A backfilled entry becomes the last occurrence only when its `OccurredAt` is later than the other entries. A recorded zero is not an entry, so it does not become the last occurrence and does not clear an older one. Logan Motrin uses Last occurrence and reads "Last: 6:42 AM" on the day it was given.

Undo and delete recompute the line from the entries that remain. Removing the only entry today changes a Done today summary from "Done today" to "Not done today". Removing the latest entry moves Last occurrence to the previous one, or to "Never logged" when none remain.

## Summaries

- **Summary display** on the log-control line: Times today, Done today, or Last occurrence. Times today is the default. The three wordings are under Summary display.
- **Count per day** over the last 30 days, where an unlogged day stays blank (phase 2). This list does not follow summary display.
- **Historical count chart** on the tracker page: events per day for the last 30 local days. Week and month are not built. Unlogged days stay blank.
- **Overlays** of one related tracker's events on that chart, as vertical shaded bands. One overlay at a time. Event-relative summaries are not part of this.
- **Values and durations over time** (built in phase 4), on the same kind of chart. Overlays apply there too.
- **Event-relative summaries** (later; see Later ideas).
- **Period progress** on a tracker that has a schedule (later; see Later ideas). It is not a fourth summary display, and it is not part of the follow-on after phase 2.

## Installable web app

Home-screen install is part of the first version because it is just static files. A phone sees a short offer on the logged-out home and the dashboard: a button when the browser can install, otherwise the Share or browser-menu steps. Phases 0–5 ship no service worker, so a deploy does not leave stale cached pages. Add a minimal worker only if a real phone lacks the install option, and that fallback does not cache pages. The later offline-logging enhancement is when a caching worker is planned: it stores the shell and enough of the already loaded trackers to log, and while online it prefers the network. The install option only appears over HTTPS, so it is verified on trackanything.io after the phase 2 deploy.

## Business model

- **Free**: the whole app. Ad-supported once ads launch.
- **Ad-free**: **$5/year** (may change). The price lives in Paddle as a Price, and the app only knows its ID (`PADDLE_PRICE_ID`), so changing the price is a Paddle dashboard change plus a config change, not a code change. Removes ads; whether it includes anything else is undecided.

**Fees.** Paddle charges about 5% + 50¢ per transaction, so a $5 sale nets about $4.25 (roughly 15% in fees). That's higher than Stripe's ~9% at this price. The difference pays for Paddle being the merchant of record: it calculates, collects, and remits sales tax and VAT worldwide, and handles invoices, refunds, and chargebacks. For a tiny product sold to EU and US customers, that's worth it. The superseded $1/month idea would have lost over half to fees.

### Ads (phase 5, after the app is live)

- **Google AdSense.** AdSense reviews the site before approving it and expects substantive original content, clear navigation, and privacy and policy pages. The logged-out homepage and a privacy page need to be real by then.
- **Consent**: AdSense requires a Google-certified consent management platform for visitors in the EEA, UK, and Switzerland. Use one (Google's own "Privacy & messaging" is the default candidate) instead of a home-made banner. The consent platform stores the visitor's choice; the app doesn't.
- The session cookie is strictly necessary and doesn't wait on consent. Ad cookies and scripts do.
- Ad-free users, and pages that shouldn't carry ads (share pages, login, settings), get no ads at all.
- Paying users keep a 2-day grace period past the end of the subscription so a late renewal doesn't flash ads.

### Subscription (phase 5)

Paddle Billing with one yearly price. Checkout, card, tax, invoices, and cancellation happen in Paddle, not in screens we build. Paddle reviews the business before live payments and expects a live site with pricing, terms of service, a privacy policy, and a refund policy. Apply early in phase 5, since review takes time. Implementation is in `ARCHITECTURE.md`.

## Phases

Each phase ends with the app running and usable locally. Phase 2 ends with the first public deploy. Behaviors described here are covered by `make test`; the suite is specified in `ARCHITECTURE.md`. Basic offline logging sits under Later ideas. It is outside these phases. Recurring, scheduled, and goal-based trackers sit there too. They are a direction for a later phase, not work in phases 0–5 or in the summary-display follow-on.

### Phase 0: Project skeleton (done)

A runnable app with a placeholder home page and a home-screen install manifest.

**Done when:** `make run` serves the placeholder page at http://localhost:8080 and `make test` passes.

### Phase 1: User accounts (done)

Signup, password login, magic-link login, logout, and settings for password and time zone. The signup form does not ask for a time zone.

**Done when:** you can sign up, log out, log back in with a password and with a magic link, and a second account works independently.

### Phase 2: The dog tracker, shared (done)

The first screen replaces paper tick marks: create a tracker, choose an icon, log now, correct a mistake, and read recent history. Two people in the house use it from their phones, see today's count, deliberately record zero meals, tell that apart from a day nobody logged, fix recent mistakes, and look back over recent days. Phase 2 is the smallest version that does that well. Historical charts and overlays are specified above and were built in phase 3. Workout measurements stay in phase 4.

- Households, count trackers, and entries. A personal household named "My trackers" is created at signup. Accounts from phase 1 get the same household when phase 2 ships. Phase 2 does not create or rename households. Those arrived in the Household organization follow-on below.
- Create, edit, archive, and restore trackers (owners). Create and edit set the name, icon, accent, and log label. The icon defaults to the tally mark, the accent defaults to none, and the label defaults to "+ Log". Summary display was not on this form. It arrived in the follow-on below. Permanent deletion waits for phase 5.
- Dashboard grouped by household. Each tracker is a card: name, icon, optional accent, the Times today summary ("3 times today", "1 time today", "None today", or "Nothing logged today"), and the log button. **Undo** shows immediately. Done today and Last occurrence arrived in the follow-on after phase 2.
- A recorded zero ("none") is separate from a day with nothing logged. Logging an event clears that day's recorded zero.
- Tracker page: today's log control and today's entries first, then history. Anyone in the household can log now with an optional note, and undo while recent. Owners can also choose the time (backfill), edit an entry's time or note, and delete. The last 30 days show each day's count, a recorded zero, or nothing logged.
- On a phone, the log control stays first. On a wider screen, history can sit beside it. Phase 2 did not put a chart on this page.
- Household page: invite link (create, regenerate, disable), join flow, remove members, promote a member to owner. Owners can't remove themselves or other owners; there is no demotion, and phase 2 has no leaving. A collapsed **Archived trackers** section restores archived trackers.
- Share links: turn on, regenerate, turn off; share page with the same summary line as the card, log button, record-none for today, and undo; no login. In phase 2 that line is the Times today wording.
- **First public deploy**, with a confirmed home-screen install on a phone. Before it goes out, a production email relay is chosen and delivering magic links, and a backup method and destination are chosen, configured, and tested with a restore. Backups run from day one. Neither choice blocks building the rest of phase 2. Hosting steps are in `ARCHITECTURE.md`.
- **Cloudflare Turnstile** protects signup, login (password and magic link), and change password. Production requires `TURNSTILE_SITE_KEY` and `TURNSTILE_SECRET_KEY`. The trackanything.io widget is Managed mode. Verification is a server-side Siteverify call. Local development uses Cloudflare's always-pass test keys when those variables are unset. Turnstile does not replace rate limits or CSRF. There is no Turnstile SDK.

**Done when:** both of you log the dog's meals from your own phones (or one of you through the share link) for a week instead of using paper, and the Dog ate card shows "3 times today" correctly in your time zone. That wording is Times today, the default. The other two summary displays are not part of this done-when.

### After phase 2: Summary display (done)

A small enhancement from dogfooding phase 2. It is not required for phase 2 to be done, and it does not wait for charts (phase 3) or for number and duration trackers (phase 4). How it was built is `SUMMARY-DISPLAY.md`.

- Add **summary display** to create and edit, with Times today, Done today, and Last occurrence. The wording for each is under Summary display. The form uses ordinary radios. The stored values are `times`, `done`, and `last`.
- Existing trackers, and any tracker saved without a choice, stay Times today. A phase 2 card keeps "3 times today".
- The log label stays a separate field.
- Done today still stores every timestamped entry. Last occurrence still shows the latest entry on a later day, or "Never logged" when the tracker has no entries.
- No new tracker kind, and no change to history rows or charts.

**Done when:** Millie ate reads "2 times today", McGill Big 3 reads "Done today" after one log and still after a second log the same day, and Logan Motrin reads "Last: 6:42 AM" on that day and "Last: Yesterday, 8:15 PM" the next day. The entries underneath are unchanged.

### After phase 2: Household organization (done)

A small follow-on from dogfooding phase 2. Inviting a spouse into "My trackers" shared every tracker in it, including ones meant to stay private. The fix is to organize trackers into households. The sharing model stays the same: households, members, owners, and one household per tracker. This follow-on does not depend on summary display, charts, or phase 4. The product shape is under Sharing.

- **Create a household.** Any logged-in user can create one with a name and becomes its owner. Typical names are Family, Home, or Millie. Then invite people with the existing invite link. The name cannot match another household that person already owns, after trimming surrounding whitespace and ignoring case.
- **Rename a household.** Owners can rename it, including their personal "My trackers". The same owned-name rule applies. Renaming a household to its own current name, including a change of spacing or case, is fine.
- **Move a tracker** to another household the same person owns. It is the same tracker with all of its entries, recorded zeros, and history. Nothing is copied. It still belongs to exactly one household. After the move, membership in the new household decides who can see it: members of the old household who are not in the new one lose access, and members of the new one gain it. Its settings stay as they are. An active share link is kept and keeps working. The move does not turn it off or replace it. This is how Millie ate and Woods run go from "My trackers" into a new Family household without being recreated.
- **Leave a household.** A regular member can leave a household. Owners cannot leave this way.
- **Dashboard headings** show only the household name. The creator's email appears only when two households you can see have the same name. See Sharing.

Not in this follow-on: deleting a household, demoting an owner, an owner leaving, ownership transfer, merging households, per-tracker permissions, nested households, family roles, invitations by email, and separate private and shared tracker types.

**Done when:** each of you keeps private trackers in your own "My trackers", Millie ate and Woods run have moved into a shared Family household with their full history, both of you log them there, and neither of you sees the other's private trackers.

### Phase 3: Historical charts, then overlays (done)

Basic time series first. A related event on that chart only after the single-tracker chart exists. Event-relative summaries stay a later idea.

- **Count chart (built).** Chart.js line of counts per local day, on the tracker page after today's logging and before history (beside that column on a wide screen, still above history). The range is the last 30 local days, including today. Unlogged days are gaps. Recorded zeros are zeros. Chart.js loads for that trend and is not required to log. Week and month aggregation are not built.
- **Overlays (built).** One other tracker you can see, chosen on this page. Its events are narrow vertical shaded bands on that same chart, one band per local day that has an event. The picker shows each tracker's icon and name, plus None. The choice is `?overlay=` on `GET /trackers/{id}` and is not stored. Dog meals and woods outings are the test case. The marks sit on the meals chart so you can look at the following one to three days. The page describes the pattern and does not claim a cause. There is no `/charts` page.
- Not in this phase: lining up repeated events and summarizing another tracker on the days before and after. Number and duration charts were built in phase 4.

**Done when:** Dog ate has a meals-per-day chart, and a woods outing can be marked on it so you can see whether low-eating days follow.

### Phase 4: Number and duration trackers (done)

- **Number and duration trackers (built).** A number tracker stores one unit on the tracker, such as lb or °F. A duration is entered as hours, minutes, and seconds and stored as seconds. Both use the same cards, detail page, and history. Share links can log them too, without a note or a chosen time.
- **Carry forward and Log again (built).** The value of the latest entry by `OccurredAt` is already in the form, and **Log again** records that value at the current time. The note does not carry forward. There is no Log again until a value exists. The server reads the carried value itself.
- **Charts (built).** Values and durations over the same last 30 local days, in the same place as the count trend. Each entry is its own point. Overlays are still one other count tracker's event days, as vertical shaded bands.
- **CSV export (built).** An owner downloads one tracker's history from the tracker page. A recorded zero is its own row.

**Done when:** woods outings record how long they were, a movement like "Club mill" tracks weight over time, and re-logging last time's weight is one tap. Then decide, from real use, whether workouts need fields and sub-trackers.

### Phase 5: Ads and the ad-free subscription

After the app is live and has real content for AdSense review.

- Privacy policy page and a real logged-out homepage.
- Account deletion in settings: removes the user's data for real, not a soft delete. A sole owner of a household with other members must promote another owner or delete the household first. The privacy policy notes how long backups keep deleted data.
- Permanent tracker deletion: an owner can delete a tracker and all its entries, with confirmation. Until then archive is the only way to remove a tracker.
- AdSense with a Google-certified consent platform. No ads on share, login, or settings pages.
- Terms of service and refund policy pages (Paddle requires them), and apply for Paddle approval early.
- Paddle checkout (sandbox first), the plan follows payment, a customer portal link in settings, and the 2-day grace period. Ad-free users see no ads.

**Done when:** AdSense approves the site, a free user sees ads after consenting in the consent platform, and a user who pays $5 sees none for a year.

## Deferred: fields and sub-trackers

Built only if number and duration trackers prove painful for workouts, for example because one exercise needs weight **and** reps **and** sets in a single entry, or because a workout should group several exercises.

- **Fields**: a tracker gets typed fields (number, text, choice, checkbox); each entry stores one value per field. Carry-forward and **Log again** extend to all fields.
- **Sub-trackers and sessions**: **Start workout** creates a session; each exercise logged in it belongs to that session. A workout that should group several exercises becomes a parent with name-only sub-trackers, and the session is complete when each has an entry. McGill Big 3 as one tracker that reads "Done today" is the Done today summary display, not this.
- **Repeat last session**: carry-forward for a whole workout.

Example of what that would record:

```
Trackers                          Entries (Saturday)
─────────                         ──────────────────
TOI                               #10 TOI            08:00
├── Swing   (implement, weight,   ├── #11 Swing      08:05  KB, 24kg, 10 reps, 3 sets
│            reps, sets)          ├── #12 Mill       08:15  Club, 10lb, 8 reps, 3 sets
├── Mill    (same fields)         └── #13 Halo       08:25  Mace, 10lb, 10 reps, 2 sets
└── Halo    (same fields)
```

## Later ideas

### Basic offline logging

A later enhancement. It is outside phases 0–5. It covers logging, and leaves the rest of the app online. Someone who has already loaded Track Anything can still tap the primary log button when the network is gone. Previously loaded trackers are cached with enough to identify them and log: name, icon, accent, summary, and the log button. A share page that is already loaded is the same action for that one tracker.

Recording stays a one-tap action, the same tap as when the app is online. The device, at the moment of the tap:

- Takes the occurrence time from the device clock.
- Makes a unique id for that entry.
- Stores the entry locally, waiting to sync.
- Updates the summary immediately, as if the entry were recorded, and shows a quiet pending mark. If today's entries are already on the page, the new row appears there with the same mark.

When the open app can reach the server again, pending entries sync on their own, and a later open finishes anything still waiting. Sending the same entry again does not create a second row. The server keeps the tap time as when it happened. The later arrival is when the row was written, which is what the undo window already uses. If a sync fails, the entry stays queued and the card keeps showing that it has not synced. The next log is still one tap.

This slice includes:

- Recognizing previously loaded trackers.
- The card's one-tap log, including the value that tap already repeats once number and duration trackers exist.
- The immediate summary update.
- A local queue, and automatic sync when the open app can reach the server again.
- A small pending or failed-sync mark.

This slice leaves these online: history, charts, creating and editing trackers, households, settings, account and login, and other administration. A note, a chosen time, record none, and undo stay online too. Undo applies after the entry has synced, under the existing 15-minute rule.

How the pending mark looks is in `DESIGN.md`. How the cache, queue, and sync work is in `ARCHITECTURE.md`.

### Recurring, scheduled, and goal-based trackers

A later phase, after the core tracker has been built and dogfooded. It is outside phases 0–5 and outside the summary-display follow-on. What follows is a design direction. It is not an implementation specification. Schedule shape, storage, the notification channel, and the exact screens stay open until creating a tracker, logging, summary display, history, charts, and number and duration entries have been in real use. Do not pick a library, a database schema, a push service, or a background job for this idea now.

A tracker today is something a person records whenever it happens. Later, a tracker may optionally carry a recurrence or an expectation. A tracker without one stays as it is: log the event when it occurs.

These stay ordinary Track Anything trackers. The schedule is optional metadata on the tracker and event model that already exists. Events remain timestamped entries. There is no recurring kind, no goal kind, and no habit kind. Dog meals, a woods outing, and a workout measurement do not gain a schedule unless someone later puts one on that tracker.

What that metadata might express:

- **Daily.** "Take medication" — once per day.
- **Weekly.** "Water plants" — once sometime during the week.
- **Weekly goal.** "Work out" — 3 times per week.
- **Specific days.** "Take out trash" — every Thursday.
- **Longer intervals.** "Change furnace filter" — every 3 months.

A simple model to explore when this is designed for real:

- **No schedule.** Record whenever the event occurs. This stays the default, and it is every tracker in the phases above.
- **Frequency.** Some number of times in a period: per day, per week, or per month.
- **Specific schedule.** Selected days, or an interval such as every three months.
- **Optional reminder.** Notify the person when it would help. A schedule does not require a reminder.

A goal is still those same events, read inside the current period. The card might show progress in that window. This is an illustration of the idea, not a wording to implement:

```
Work out
2 / 3 this week
```

When the next period begins, the line resets because the events are counted in the new window. The old events stay as they were. History and a later chart still show them. Nothing is rewritten at the boundary.

Reminders stay small on purpose. They should help someone keep a record, and they should not turn Track Anything into a task manager. A useful shape is a conditional reminder: "Remind me if I haven't logged this by Friday." It follows the tracker's state. If the activity is already logged for that period, there is nothing to send. A reminder that fires even after the person has logged it is the shape to avoid. How a reminder would be delivered — email or anything else — is unset, including whether it needs more than the app already has.

How this could eventually meet the rest of the product, once there is a reason to build it:

- **Dashboard.** The card is still a name, an icon, a summary, and one log button. Progress in the current period could be that summary when the tracker has a schedule. Times today, Done today, and Last occurrence remain the summary display for a tracker that does not. Logging stays one tap.
- **Icons.** The same icon and accent. A schedule does not get its own symbol. The name still identifies the tracker.
- **Charts and history.** History stays dated events, including a recorded zero versus nothing logged. A period goal is a way of reading those events, not a second record. Charts stay a picture of what was logged.
- **Offline event recording.** The later one-tap log still records an event at the time of the tap. When it syncs, it counts toward the window that contains that time, the same as an event logged online. Changing a schedule, and anything about reminders, stays online with the rest of administration.
- **Shared trackers.** Household members and a share link still log events under the rules in Sharing. Who may edit a schedule, and who a reminder is for, stays open. A share link does not become an assignment.
- **Notifications.** Only the optional conditional reminder above. No inbox and no general alerts. The channel is part of what stays undecided.
- **Streaks.** Streaks stay unplanned, and gamification stays a non-goal. If a concrete case ever brings them back, they would be another reading of the same events. This idea does not add them.

The product stays a small, flexible way to record arbitrary events and measurements. A schedule, when someone wants one, makes that record easier to keep. It does not make Track Anything a calendar, a todo list, or a habit-tracking platform.

- **Event-relative view.** After historical charts and overlays have real history behind them, align repeated occurrences of an event and show another tracker's values on the surrounding days. For woods and meals, that is the average meal count at three days before, two days before, one day before, the woods day, and one, two, and three days after, across outings. A decline that shows up one or two days after woods is easier to see than on the calendar timeline. This is still a picture for a person to read. It is not a correlation study, and it is not designed further until charts and overlays are in use.
- **Start without an account**: the logged-out homepage asks "What would you like to track?", and submitting it creates a temporary user and the tracker; saving the account later adds an email. Needs long-lived sessions, cleanup of abandoned temporary users, a rate limit, and a visible **Log in** link so returning users don't make duplicates. Share links already cover much of the "use it without signing up" need.
- Tracker templates ("Start from: McGill Big 3", "Start from: TOI").
- A user-uploaded icon per tracker. The first version uses the built-in picker only.
- CSV import.
- Extra things the ad-free plan could include, if anything.

## Decision log

| Topic | Current decision | Notes |
| --- | --- | --- |
| Price | $5/year, may change | $1/month superseded. The price lives in Paddle; the app stores only `PADDLE_PRICE_ID` |
| Payments | Paddle Billing | Merchant of record handles sales tax and VAT; ~15% fees at $5 accepted for that |
| Sharing | Households plus per-tracker share links | Share links work without logging in; non-owners can log now, mark today as none, and undo recent entries. Only owners choose an entry's time on an ordinary post. Later offline sync stores the tap time |
| Households in phase 2 | Personal "My trackers" household, invites only | Phase 2 had no household creation or renaming. Owners invite, remove members, and promote; no self-removal, demotion, or leaving |
| Household organization | Shipped after phase 2, from dogfooding | Personal households stay private by default. Shared trackers live in a separately created household. Create, rename, move a tracker between households you own, and member leave. Names are not globally unique, but one person cannot own two that match after trim and case folding. A move keeps an active share link. Same `Household` model, no personal-tracker type. Creator email only when visible names collide. Household deletion, owner leave, demotion, and transfer stay deferred |
| Tracker removal | Archive and restore; permanent delete in phase 5 | Restore lives in a collapsed section on the household page |
| Streaks | Not planned | No clear definition, and gamification is a non-goal. Reconsider only for a concrete use case. A later schedule does not add them |
| Schedules and reminders | Later direction, outside phases 0–5 | Optional metadata on an ordinary tracker: no schedule, a frequency, or selected days or an interval, plus an optional conditional reminder. Not a new kind. Not a calendar, a todo list, or a habit platform. Storage, delivery, and exact wording stay open until the core tracker is dogfooded. See Later ideas |
| Invites | Copyable links | The app doesn't send invite email |
| Login | Email first; password optional | Magic link is the primary path. A password is set in settings after the email is confirmed. See `DESIGN.md` |
| Time zone | Detect once at signup, then keep it | Calendar days do not follow the browser after signup. Friendly name in settings if it needs changing. Mechanism in `ARCHITECTURE.md` |
| First use case | Dog meals | Replaces paper tick marks |
| Second use case | Woods outings and workout progression | A woods outing can be a count, for the phase 3 overlay, or a duration. Workout weight is a number tracker. Both shipped in phase 4. |
| Tracker kinds | Count first, then number and duration | Fields and sub-trackers deferred until real use demands them |
| Interface | Calm mobile-first notebook | Cards with name, icon, optional accent, summary, and a one-tap log. Recognizable icons and soft cards, with less on screen than a family calendar. |
| Tracker icon | Optional, in phase 2 | Built-in picker of clear icons or emoji. Default is the tally mark. Name stays visible. Uploads stay a later idea. Moved out of later ideas so the first tracker is recognizable on its card. |
| Accent | Optional, small built-in set | Recognition on the card and icon only. Pico remains the only UI palette. Default is none. |
| Log button | Short label, still one tap | Default "+ Log". The dog card can say "+ Ate". Logan Motrin can say "Gave Motrin". Same quick-log and 15-minute undo as the earlier "+1" label. Independent of summary display. |
| Summary display | Times today, Done today, or Last occurrence | Stored as `times`, `done`, or `last`. Presentation of the same timestamped entries. Default `times`. Shipped after phase 2. Done today does not cap entries per day. Last occurrence crosses midnight. An empty tracker says "Never logged". See Summary display. |
| Zero vs unlogged | Different states | A recorded zero is deliberate. No entries and no recorded zero means nothing logged. Charts leave that day blank. The Times today summary says "None today" or "Nothing logged today". Done today and Last occurrence do not treat the zero as an entry. |
| Analysis | Daily count chart, then overlay, then value charts | The count line chart is on the tracker page: last 30 local days, including today, gaps for unlogged days, zeros for recorded none. Number and duration charts use that same window and keep each entry as its own point. One other count tracker's events are vertical shaded bands on any of those charts, one band per local day, chosen with `?overlay=` and not stored. Week and month buckets are not built. Event-relative summaries are a later idea, not a statistics feature. Patterns, not causes. There is no `/charts` page. |
| Ads | AdSense with a certified consent platform, after launch | No home-made consent banner. No ads on share, login, or settings |
| Start without account | Later idea | Share links cover most of the need for now |
| Install | Manifest + icons; phone offer on home and dashboard; no service worker through phase 5 | Verified on a phone at first deploy. Not a store app. A small worker arrives with later offline logging |
| Offline logging | Later, primary log action only | Outside phases 0–5. Previously loaded trackers, one tap, local queue, automatic idempotent sync. The tap time is when it happened. See Later ideas |
| First public deploy | End of phase 2 | Backups live from the start. Email relay and backup method are picked before deploy, not before building |
| Server metrics | Not built | A Prometheus scrape is a later idea, not part of the current app and not required for the first deploy. The intended shape is in `ARCHITECTURE.md` under Metrics |

## Open questions

- **Share page contents**: the summary line, the log button, and today's entries only (current plan), or the recent history too?
- **Undo window**: 15 minutes is the starting value. Right length?
- **Workouts**: is one number per tracker (e.g. weight for "Club mill") enough, or do weight and reps need to be recorded together? That's the trigger for fields. Relatedly, for TOI: one entry per exercise with a sets count, or one entry per set?
- **Units**: fixed per tracker (current plan), or chosen per entry?
- **Free tier**: ad-supported, limited by tracker count or history, or simply generous so $5/year is mostly a convenience purchase?
- **Does ad-free include anything else?**
- **Consent platform**: Google's "Privacy & messaging", or a third-party certified one?
- **Deleted data in backups**: how long the privacy policy says backups keep it. The backup mechanism is in `ARCHITECTURE.md`.
