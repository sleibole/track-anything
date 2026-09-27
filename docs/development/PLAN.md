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
- Do not add a frontend framework or a product-analytics stack in order to draw charts. Chart.js on the existing pages is enough (`ARCHITECTURE.md`). Operational metrics for the server are collected in Prometheus; that is specified in `ARCHITECTURE.md` and is not a charting tool.
- Do not become a habit-coaching or gamification platform unless real users clearly demand it.
- Do not sacrifice the one-tap tracking experience for generic configurability.
- No native mobile apps. Home-screen install is a web app manifest, not a store app.
- No offline use. Logging still talks to the server.
- No public API.
- Keep the implementation small. The engineering limits (no microservices, no schema language, no scale work before there are users) are in `ARCHITECTURE.md`.

## Reference use cases

**Dog eating and GI trouble.** Log each time the dog eats. Some days she eats three or four times; occasionally she eats very little or not at all, and she may have stomach noises or diarrhea. Those days may follow off-leash time in the woods, where she could eat something she found. A second tracker records a woods outing. Marking that outing on the meals-per-day chart makes the one to three days afterward easy to inspect. The inputs stay simple. How the chart works is under Charts and overlays. The dog is the test case for overlays in general, not a separate feature.

**Workout progression.** Movements, load, duration, or reps over time, to see progression in club, kettlebell, and related training (e.g. Mark Wildman's TOI program, McGill's Big 3) without turning Track Anything into a fitness app.

## Tracking model

Start with the smallest useful tracker and add value shapes only when a real use case needs them.

| Kind | Each entry records | Examples | Arrives in |
| --- | --- | --- | --- |
| Count | A timestamp | Dog ate, medication taken, symptom occurred | Phase 2 |
| Number | A timestamp and a number with the tracker's unit | Body weight, kettlebell weight, temperature | Phase 4 |
| Duration | A timestamp and a length of time | Off-leash time, workout length | Phase 4 |

- Any entry can have an optional **note**, but the note never slows down the normal one-tap path.
- Each tracker has an optional **icon** and an optional **accent**. The icon comes from a small built-in picker of clear icons or emoji and defaults to the tally-mark icon. The accent comes from a small built-in set of soft colors and defaults to none. Both can be changed later. The name is always shown with them. Someone picking a tracker can tell it by the name alone. User-uploaded icons are a later idea, not part of the first version.
- The card's logging button uses a short **log label**. The default is "+ Log". It can be changed to a few words such as "+ Ate". The tap still records the current time.
- A count tracker can store one **recorded zero** for a calendar day: the count is deliberately none. That mark is not an event and does not increase the count. A day with no entries and no recorded zero is **nothing logged**. Logging an event for that day clears the recorded zero. Charts and per-day counts draw a recorded zero as zero and leave an unlogged day blank.
- For number trackers, **the last value carries forward**: if you used a 5lb club last time, the form already says 5, and **Log again** records it with one tap. Backfilling an older entry does not change what carries forward. The note does not carry forward, and the time defaults to now. Changing the prefilled value (moving up to 6lb) and submitting is how a new value starts carrying forward.
- Members log at the current time, with an optional note. Owners can also choose when an entry happened, which is how they backfill ("I forgot to log yesterday"). When it happened is separate from when the row was written. Undo uses when it was written. Marking an earlier day as none is the same kind of backfill: owner only.
- **Archive, not delete**, for trackers. Archiving hides the tracker and is the normal way to say "I don't want this tracker anymore." It is reversible: the owner restores it from the household page. Permanently deleting a tracker and its entries is a separate, confirmed owner action that arrives in phase 5, with the other destructive privacy operations.
- **Fields and sub-trackers** (one workout containing several exercises, each with weight, reps, and sets) are designed below but deferred. They get built only if number and duration trackers prove painful for workouts in real use.

## Sharing

Trackers belong to a **household**, and a single tracker can also be shared by **link**.

**Households.** Every user gets a personal household at signup, named "My trackers", with that user as owner. The owner can invite people with an **invite link** (copied and sent by hand; the app sends no invite email). Opening it asks you to log in or sign up, then adds you as a member. You can belong to several households; the dashboard shows trackers from all of them, grouped by household. A household someone else created shows that person's email beside its name, so two households called "My trackers" stay distinct. Phase 2 has no screens for creating or renaming households.

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

- **Recent** means an entry or recorded zero created in the last 15 minutes, by when it was saved, not when it happened. That covers "oops, I tapped twice" without letting a link holder wipe history. The window is a constant, easy to change.
- The server enforces the time rule: a member's entry always happens now, even if a request supplies a different time.
- A household can have more than one owner, so ownership can be shared.
- Ownership management stays small in phase 2. Removing applies to members only: an owner cannot remove themselves or another owner. There is no demotion and no "leave household". Phase 5 account deletion handles the sole-owner cases.
- **Regenerating** a share link or invite link makes the old one stop working immediately. Turning a share link off does the same. Archiving a tracker turns its share link off, and restoring it leaves sharing off until the owner turns on a new link.
- Members who want their own trackers make them in their personal household, where they are the owner.
- Entries remember who logged them, or that they came in through a share link. The UI can show that where it helps without cluttering every view.
- A tracker you can't see is indistinguishable from one that doesn't exist.

## Accounts and login

Two ways in, same account. The screens lead with email. A password is optional and secondary. See `DESIGN.md` for how that is presented.

- **Magic link.** Enter your email, get a one-time link valid for 15 minutes. Opening it shows a **Log in as you@example.com** button, and pressing that logs you in. The extra press is deliberate: email security scanners open links before the person does, and a link that logged in on open would be used up by the scanner. The same mechanism handles "forgot password": log in with a link, then set a new password in settings. Changing the password logs out every other session.
- **Email and password.** Password is optional; set it from the secondary signup or login path, or later in settings.
- Requesting a link shows the same "check your email" page whether or not the account exists, so the form can't be used to find out who has an account.

Signing up needs only an email. Magic links double as email verification.

Sessions last 30 days. Once a session is past half its lifetime, the next request renews it for another 30 days, so active users stay logged in.

## Calendar days

"Today" and which day an entry belongs to follow a time zone captured when the account is created. Traveling does not move entries onto different days. Signup does not ask for a zone. If it is wrong, settings offers a low-profile friendly name, not a raw zone identifier. People on a share link have no zone of their own, so a household shares one "today". The storage and detection rules are in `ARCHITECTURE.md`.

## Interface

A calm, mobile-first tracking notebook. Logging is fast, review is easy, and the same screens cover the dog's meals now and a workout measurement in phase 4. Tone takes a little from a family calendar: a recognizable icon, a soft color, an approachable card. The layout stays sparser than a calendar. How the screens look is in `DESIGN.md`.

**Home.** Trackers stay grouped by household. Each one is a card with its name, icon, optional accent, a current summary, and one logging button. The dog card reads "Dog eating — 2 times today" with **+ Ate**. A count summary says "1 time today" or "2 times today", "None today" when a zero was recorded, or "Nothing logged today". The button records the current time in one tap, then offers **Undo**.

**Tracker detail.** Today's logging control and today's entries come first. History and trends follow. Correcting a mistake sits on the entry. Anyone who can log can undo an entry or a recorded zero from the last 15 minutes. Only an owner can choose when a new entry happened, edit the time or note, delete an older entry, mark an earlier day as none, or clear an older recorded zero. On a phone the logging action stays first. On a wider screen, history and charts can sit beside that column. They never sit above it.

**History.** Dated entries, with the time they happened. "None" (a recorded zero) and "Nothing logged" are different rows. That distinction matters for the dog: a day with no meals is data, and a day nobody wrote down is a gap.

**Trends.** The chart matches the tracker: events per day, a value over time, or a duration over time. A recorded zero is a zero. A day with nothing logged is a gap. That historical chart is on this page. Overlaying another tracker's events is a later phase 3 view. See Charts and overlays.

**First slice.** Phase 2 replaces paper tick marks: create a tracker, choose an icon, log an event with the current time, correct a mistake, and review recent history, including a recorded zero versus nothing logged. An accent and a custom log label can be set then too; both are optional. Richer workout inputs stay in phase 4. Phase 3 is a historical chart first, then overlays of related events. Event-relative summaries are a later idea.

## Charts and overlays

Charts stay an extension of tracking. The sequence is:

1. Store simple observations and events.
2. Show a straightforward historical chart of one tracker.
3. Draw a related event on that chart.
4. Let the person looking at it notice the pattern.
5. Later, once there is enough history, summarize another tracker on the days before and after a repeated event.

That last step is the event-relative view under Later ideas. It is not part of the first charts, and it is not a statistics or correlation feature.

Chart.js draws the charts. It fits the server-rendered pages and HTMX, so visualization does not need a frontend framework. How it is loaded is in `ARCHITECTURE.md`. Layout is in `DESIGN.md`.

Charts stay secondary to recording. The log control comes first, a trend never sits above it, and logging works if the chart does not load. The historical chart lives on the tracker page, below the log control or beside it on a wide screen. The overlay view comes only after that chart exists: controls first on a phone, the chart beside them on a wide screen.

- **Historical chart.** One tracker over time. Counts are events per day, week, or month (meals per day). Numbers are a value over time (workout weight). Durations are length over time. A recorded zero is drawn as zero. A day with nothing logged is left blank. Count charts are phase 3. Number and duration charts are phase 4, in the same place.
- **Overlay.** Events from another tracker you can see, drawn on that historical chart. A woods outing can appear as a marker, an annotation, or a shaded vertical region. Which treatment is still open. The picker shows the icon and the name. Dog eating and woods outings, in Reference use cases, are the test case: you look at the one to three days after an outing. The page describes what is on the chart and never claims a cause.
- The overlay is general. The same chart could later mark a workout against later soreness or recovery, alcohol against sleep, coffee against anxiety or energy, a medication against symptom frequency, or a late bedtime against next-day energy.

## Summaries

- **Count today** ("N times today", "1 time today", "None today", or "Nothing logged today") and **count per day** over the last 30 days, where an unlogged day stays blank (phase 2).
- **Historical charts** for counts (phase 3).
- **Overlays** of a related tracker's events on that chart (phase 3, after the historical chart).
- **Values and durations over time** (phase 4), on the same kind of chart. Overlays apply there too.
- **Event-relative summaries** (later; see Later ideas).

## Installable web app

Home-screen install is part of the first version because it is just static files. No service worker: skipping one avoids stale cached pages after a deploy. Add a minimal worker only if a real phone lacks the install option. The install option only appears over HTTPS, so it is verified on trackanything.io after the phase 2 deploy.

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

Each phase ends with the app running and usable locally. Phase 2 ends with the first public deploy. Behaviors described here are covered by `make test`; the suite is specified in `ARCHITECTURE.md`.

### Phase 0: Project skeleton (done)

A runnable app with a placeholder home page and a home-screen install manifest.

**Done when:** `make run` serves the placeholder page at http://localhost:8080 and `make test` passes.

### Phase 1: User accounts (done)

Signup, password login, magic-link login, logout, and settings for password and time zone. The signup form does not ask for a time zone.

**Done when:** you can sign up, log out, log back in with a password and with a magic link, and a second account works independently.

### Phase 2: The dog tracker, shared

The first screen replaces paper tick marks: create a tracker, choose an icon, log now, correct a mistake, and read recent history. Two people in the house use it from their phones, see today's count, deliberately record zero meals, tell that apart from a day nobody logged, fix recent mistakes, and look back over recent days. Phase 2 is the smallest version that does that well. Historical charts and overlays are specified above and stay in phase 3. Workout measurements stay in phase 4.

- Households, count trackers, and entries. A personal household named "My trackers" is created at signup. Accounts from phase 1 get the same household when phase 2 ships. No household creation or renaming.
- Create, edit, archive, and restore trackers (owners). Create and edit set the name, icon, accent, and log label. The icon defaults to the tally mark, the accent defaults to none, and the label defaults to "+ Log". Permanent deletion waits for phase 5.
- Dashboard grouped by household. Each tracker is a card: name, icon, optional accent, today's summary ("3 times today", "None today", or "Nothing logged today"), and the log button. **Undo** shows immediately.
- A recorded zero ("none") is separate from a day with nothing logged. Logging an event clears that day's recorded zero.
- Tracker page: today's log control and today's entries first, then history. Anyone in the household can log now with an optional note, and undo while recent. Owners can also choose the time (backfill), edit an entry's time or note, and delete. The last 30 days show each day's count, a recorded zero, or nothing logged.
- On a phone, the log control stays first. On a wider screen, history can sit beside it. There is no chart on this page yet.
- Household page: invite link (create, regenerate, disable), join flow, remove members, promote a member to owner. Owners can't remove themselves or other owners; there is no demotion and no leaving. A collapsed **Archived trackers** section restores archived trackers.
- Share links: turn on, regenerate, turn off; share page with the same today's summary, log button, record-none for today, and undo; no login.
- **First public deploy**, with a confirmed home-screen install on a phone. Before it goes out, a production email relay is chosen and delivering magic links, and a backup method and destination are chosen, configured, and tested with a restore. Backups run from day one. Neither choice blocks building the rest of phase 2. Hosting steps are in `ARCHITECTURE.md`.

**Done when:** both of you log the dog's meals from your own phones (or one of you through the share link) for a week instead of using paper, and the Dog ate card shows "3 times today" correctly in your time zone.

### Phase 3: Historical charts, then overlays

Basic time series first. A related event on that chart only after the single-tracker chart exists. Event-relative summaries stay a later idea.

- Per-tracker Chart.js chart of counts per day, week, or month, on the tracker page after today's logging (beside it on a wide screen). Unlogged days are gaps. Recorded zeros are zeros. Chart.js loads for that trend and is not required to log.
- Then, overlay events from another tracker you can see on that chart: a marker, annotation, or shaded region (treatment still open). The picker shows each tracker's icon and name. Dog meals and woods outings are the test case. The marks sit on the meals chart so you can look at the following one to three days. The page describes the pattern and does not claim a cause.
- Not in this phase: lining up repeated events and summarizing another tracker on the days before and after.

**Done when:** Dog ate has a meals-per-day chart, and a woods outing can be marked on it so you can see whether low-eating days follow.

### Phase 4: Number and duration trackers

- Number (with a unit) or duration trackers, on the same cards, detail page, and history. Share links support them too.
- Prefill from the last entry and **Log again**. That stays the one-tap path when the last value should carry forward.
- Charts for values and durations over time (weight, or how long), in the same place on the tracker page as the count trend. Overlays apply to these charts the same way.
- CSV export per tracker.

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
- **Sub-trackers and sessions**: **Start workout** creates a session; each exercise logged in it belongs to that session. McGill Big 3 becomes a parent with three name-only sub-trackers, and "done today" means each has an entry.
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

- **Event-relative view.** After historical charts and overlays have real history behind them, align repeated occurrences of an event and show another tracker's values on the surrounding days. For woods and meals, that is the average meal count at three days before, two days before, one day before, the woods day, and one, two, and three days after, across outings. A decline that shows up one or two days after woods is easier to see than on the calendar timeline. This is still a picture for a person to read. It is not a correlation study, and it is not designed further until charts and overlays are in use.
- **Start without an account**: the logged-out homepage asks "What would you like to track?", and submitting it creates a temporary user and the tracker; saving the account later adds an email. Needs long-lived sessions, cleanup of abandoned temporary users, a rate limit, and a visible **Log in** link so returning users don't make duplicates. Share links already cover much of the "use it without signing up" need.
- Tracker templates ("Start from: McGill Big 3", "Start from: TOI").
- A user-uploaded icon per tracker. The first version uses the built-in picker only.
- Reminders (email) for daily trackers.
- CSV import.
- Extra things the ad-free plan could include, if anything.

## Decision log

| Topic | Current decision | Notes |
| --- | --- | --- |
| Price | $5/year, may change | $1/month superseded. The price lives in Paddle; the app stores only `PADDLE_PRICE_ID` |
| Payments | Paddle Billing | Merchant of record handles sales tax and VAT; ~15% fees at $5 accepted for that |
| Sharing | Households plus per-tracker share links | Share links work without logging in; non-owners can log now, mark today as none, and undo recent entries. Only owners choose an entry's time |
| Households in phase 2 | Personal "My trackers" household, invites only | No household creation or renaming. Owners invite, remove members, and promote; no self-removal, demotion, or leaving |
| Tracker removal | Archive and restore; permanent delete in phase 5 | Restore lives in a collapsed section on the household page |
| Streaks | Not planned | No clear definition, and gamification is a non-goal. Reconsider only for a concrete use case |
| Invites | Copyable links | The app doesn't send invite email |
| Login | Email first; password optional | Magic link is the primary path. Password is secondary. See `DESIGN.md` |
| Time zone | Detect once at signup, then keep it | Calendar days do not follow the browser after signup. Friendly name in settings if it needs changing. Mechanism in `ARCHITECTURE.md` |
| First use case | Dog meals | Replaces paper tick marks |
| Second use case | Woods outings and workout progression | A woods outing is the phase 3 overlay test, and a count is enough for that. Duration, and workout weight, arrive in phase 4. |
| Tracker kinds | Count first, then number and duration | Fields and sub-trackers deferred until real use demands them |
| Interface | Calm mobile-first notebook | Cards with name, icon, optional accent, summary, and a one-tap log. Recognizable icons and soft cards, with less on screen than a family calendar. |
| Tracker icon | Optional, in phase 2 | Built-in picker of clear icons or emoji. Default is the tally mark. Name stays visible. Uploads stay a later idea. Moved out of later ideas so the first tracker is recognizable on its card. |
| Accent | Optional, small built-in set | Recognition on the card and icon only. Pico remains the only UI palette. Default is none. |
| Log button | Short label, still one tap | Default "+ Log". The dog card can say "+ Ate". Same quick-log and 15-minute undo as the earlier "+1" label. |
| Zero vs unlogged | Different states | A recorded zero is deliberate. No entries and no recorded zero means nothing logged. Charts leave that day blank. |
| Analysis | Historical chart, then overlay | Chart.js time series first (phase 3), then one tracker's events marked on another's chart. Looking at the 1–3 days after a woods outing is the test case. Event-relative summaries are a later idea, not a statistics feature. Patterns, not causes. |
| Ads | AdSense with a certified consent platform, after launch | No home-made consent banner. No ads on share, login, or settings |
| Start without account | Later idea | Share links cover most of the need for now |
| Install | Manifest + icons, no service worker | Verified on a phone at first deploy. Not a store app |
| First public deploy | End of phase 2 | Backups live from the start. Email relay and backup method are picked before deploy, not before building |
| Server metrics | Prometheus | Scrape of the process for requests, latency, errors, and database health. Not product analytics. See `ARCHITECTURE.md` |

## Open questions

- **Share page contents**: today's summary, the log button, and today's entries only (current plan), or the recent history too?
- **Undo window**: 15 minutes is the starting value. Right length?
- **Workouts**: is one number per tracker (e.g. weight for "Club mill") enough, or do weight and reps need to be recorded together? That's the trigger for fields. Relatedly, for TOI: one entry per exercise with a sets count, or one entry per set?
- **Units**: fixed per tracker (current plan), or chosen per entry?
- **Free tier**: ad-supported, limited by tracker count or history, or simply generous so $5/year is mostly a convenience purchase?
- **Does ad-free include anything else?**
- **Consent platform**: Google's "Privacy & messaging", or a third-party certified one?
- **Overlay mark**: a marker, an annotation, or a shaded vertical region? Decide when overlays are built.
- **Deleted data in backups**: how long the privacy policy says backups keep it. The backup mechanism is in `ARCHITECTURE.md`.
