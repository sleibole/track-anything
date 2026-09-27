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
- **History becomes more valuable over time.** Charts, overlays, and comparisons are the payoff for consistently recording simple data.
- **Shared by default where it helps.** A tracker can be used by a whole household, or by anyone with its link, without everyone needing an account.
- **Micro-product economics.** Operating cost and complexity stay low enough that a $5/year product is viable.
- **Product is the point.** Use AI aggressively to deliver the product. Hand-building small tutorial projects is still valuable when learning Go is the goal, but Track Anything is about shipping.

## Guardrails (non-goals)

- Do not become a full fitness platform.
- Do not become a medical diagnostic product or claim that correlations imply causes.
- Do not become a habit-coaching or gamification platform unless real users clearly demand it.
- Do not sacrifice the one-tap tracking experience for generic configurability.
- No native mobile apps. Home-screen install is a web app manifest, not a store app.
- No offline use. Logging still talks to the server.
- No public API.
- Keep the implementation small. The engineering limits (no microservices, no schema language, no scale work before there are users) are in `ARCHITECTURE.md`.

## Reference use cases

**Dog eating and GI trouble.** Log each time the dog eats. Some days it's four meals, some days zero, and low intake sometimes comes with GI symptoms. A second tracker records woods/off-leash outings. The useful view isn't just both series on the same dates: it's being able to see whether a woods outing is followed by less eating or GI trouble one, two, or three days later. Simple inputs, then relationships that become visible in the history.

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
- Owners can backfill ("I forgot to log yesterday"). When it happened is separate from when the row was written. Undo uses when it was written. Marking an earlier day as none is the same kind of backfill: owner only.
- **Archive, not delete**, for trackers. Archiving is reversible and hides the tracker. Permanently deleting a tracker and its entries is a separate, confirmed owner action.
- **Fields and sub-trackers** (one workout containing several exercises, each with weight, reps, and sets) are designed below but deferred. They get built only if number and duration trackers prove painful for workouts in real use.

## Sharing

Trackers belong to a **household**, and a single tracker can also be shared by **link**.

**Households.** Every user gets a personal household at signup. The owner can invite people with an **invite link** (copied and sent by hand; the app sends no invite email). Opening it asks you to log in or sign up, then adds you as a member. You can belong to several households; the dashboard shows trackers from all of them, grouped by household.

**Share links.** The owner can turn on a share link for one tracker. Anyone with the link can use that tracker **without logging in**. That covers a kitchen tablet, a dog sitter, or a relative who will never make an account.

**What each person can do:**

| Action | Owner | Household member | Anyone with the share link |
| --- | --- | --- | --- |
| See the tracker and its recent entries | Yes | Yes | Yes (that one tracker only) |
| Log an entry | Yes | Yes | Yes |
| Undo a recent entry | Yes | Yes | Yes |
| Mark today as none, or undo that mark within 15 minutes | Yes | Yes | Yes |
| Mark an earlier day as none, or clear an older mark | Yes | No | No |
| Edit an entry's time or note, delete older entries | Yes | No | No |
| Create, edit, or archive trackers | Yes | No | No |
| Turn on, regenerate, or turn off a share link | Yes | No | No |
| Invite or remove household members, regenerate the invite link | Yes | No | No |

- **Recent** means an entry or recorded zero created in the last 15 minutes, by when it was saved, not when it happened. That covers "oops, I tapped twice" without letting a link holder wipe history. The window is a constant, easy to change.
- A household can have more than one owner, so ownership can be shared or handed over.
- **Regenerating** a share link or invite link makes the old one stop working immediately. Turning a share link off does the same. Archiving a tracker stops its share link too.
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

**Tracker detail.** Today's logging control and today's entries come first. History and trends follow. Correcting a mistake sits on the entry. Anyone who can log can undo an entry or a recorded zero from the last 15 minutes. Only an owner can edit the time or note, delete an older entry, mark an earlier day as none, or clear an older recorded zero. On a phone the logging action stays first. On a wider screen, history and charts can sit beside that column. They never sit above it.

**History.** Dated entries, with the time they happened. "None" (a recorded zero) and "Nothing logged" are different rows. That distinction matters for the dog: a day with no meals is data, and a day nobody wrote down is a gap.

**Trends.** The chart matches the tracker. Counts are events per day, such as meals. Numbers are a value over time, such as workout weight. Durations are length over time. A recorded zero is a zero on the chart. A day with nothing logged is a gap. Comparing separate trackers, including the one to three days after one of them, stays the phase 3 charts view, not the first logging screen. On a phone that view puts its controls first; on a wider screen the chart can sit beside them. Dog eating against off-leash woods time is the test case.

**First slice.** Phase 2 replaces paper tick marks: create a tracker, choose an icon, log an event with the current time, correct a mistake, and review recent history, including a recorded zero versus nothing logged. An accent and a custom log label can be set then too; both are optional. Richer workout inputs stay in phase 4. Per-tracker trends and cross-tracker charts stay in phase 3, which is already their priority.

## Charts, overlays, and delayed comparisons

The payoff for consistent tracking. Charts stay secondary to recording. The log control comes first, a trend never sits above it, and logging works if the chart does not load. Per-tracker trends live on the tracker page, below the log control or beside it on a wide screen. Comparing two trackers stays the phase 3 charts view: controls first on a phone, the chart beside them on a wide screen.

- **Per tracker**: counts per day, week, or month for count trackers (meals per day); values over time for number trackers (workout weight); duration over time for duration trackers. A recorded zero is drawn as zero. A day with nothing logged is left blank.
- **Overlay**: pick two or more trackers you can see and draw them on a shared time axis. Each tracker in the picker shows its icon and its name.
- **Delayed comparison**: shift one tracker by 1, 2, or 3 days, so a woods outing lines up with meals on the following days. Dog eating and off-leash woods time are the test case.
- The page describes what you're seeing ("meals on the 2 days after each woods outing") and never claims a cause.

## Summaries

- **Count today** ("N times today", "1 time today", "None today", or "Nothing logged today"), **count per day** over the last 30 days (an unlogged day stays blank), and **current streak** (phase 2). The streak is unchanged: it is not a way to encode "nothing logged".
- **Charts, overlays, delayed comparisons** (phase 3).
- **Values and durations over time** (phase 4).

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

The first screen replaces paper tick marks: create a tracker, choose an icon, log now, correct a mistake, and read recent history. Trends, cross-tracker charts, and workout measurements are specified above and stay in phases 3 and 4.

- Households, count trackers, and entries. A personal household is created at signup.
- Create, rename, archive, and permanently delete trackers (owners). Create and edit also set the icon, accent, and log label. The icon defaults to the tally mark, the accent defaults to none, and the label defaults to "+ Log".
- Dashboard grouped by household. Each tracker is a card: name, icon, optional accent, today's summary ("3 times today", "None today", or "Nothing logged today"), and the log button. **Undo** shows immediately.
- A recorded zero ("none") is separate from a day with nothing logged. Logging an event clears that day's recorded zero.
- Tracker page: today's log control and today's entries first, then history. Entries show their time and can be undone while recent. Owners can backfill, edit, and delete. The last 30 days show each day's count, a recorded zero, or nothing logged. The current streak stays as already planned.
- On a phone, the log control stays first. On a wider screen, history can sit beside it. There is no chart on this page yet.
- Household page: invite link (create, regenerate, disable), join flow, remove members, promote to owner.
- Share links: turn on, regenerate, turn off; share page with the same today's summary, log button, record-none for today, and undo; no login.
- **First public deploy**, including backups from day one and a confirmed home-screen install on a phone. Hosting steps are in `ARCHITECTURE.md`.

**Done when:** both of you log the dog's meals from your own phones (or one of you through the share link) for a week instead of using paper, and the Dog ate card shows "3 times today" correctly in your time zone.

### Phase 3: Charts, overlays, delayed comparisons

- Per-tracker chart of counts per day, week, or month, on the tracker page after today's logging (beside it on a wide screen). Unlogged days are gaps. Recorded zeros are zeros. Chart.js loads for that trend and is not required to log.
- Overlay any trackers you can see on a shared time axis. The picker shows each tracker's icon and name.
- Shift one tracker by 1–3 days for "what happened in the days after X?". Dog eating and off-leash woods time are the test case, including the following one to three days.
- Neutral wording: patterns, not causes.

**Done when:** you can put "Woods outing" and "Dog ate" on one chart, shift woods by 1–3 days, and see whether low-eating days follow outings.

### Phase 4: Number and duration trackers

- Number (with a unit) or duration trackers, on the same cards, detail page, and history. Share links support them too.
- Prefill from the last entry and **Log again**. That stays the one-tap path when the last value should carry forward.
- Charts for values and durations over time (weight, or how long), in the same place on the tracker page as the count trend.
- CSV export per tracker.

**Done when:** woods outings record how long they were, a movement like "Club mill" tracks weight over time, and re-logging last time's weight is one tap. Then decide, from real use, whether workouts need fields and sub-trackers.

### Phase 5: Ads and the ad-free subscription

After the app is live and has real content for AdSense review.

- Privacy policy page and a real logged-out homepage.
- Account deletion in settings: removes the user's data for real, not a soft delete. A sole owner of a household with other members must promote another owner or delete the household first. The privacy policy notes how long backups keep deleted data.
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
| Sharing | Households plus per-tracker share links | Share links work without logging in; non-owners can log, mark today as none, and undo recent entries |
| Invites | Copyable links | The app doesn't send invite email |
| Login | Email first; password optional | Magic link is the primary path. Password is secondary. See `DESIGN.md` |
| Time zone | Detect once at signup, then keep it | Calendar days do not follow the browser after signup. Friendly name in settings if it needs changing. Mechanism in `ARCHITECTURE.md` |
| First use case | Dog meals | Replaces paper tick marks |
| Second use case | Woods outings and workout progression | Validates number and duration tracking |
| Tracker kinds | Count first, then number and duration | Fields and sub-trackers deferred until real use demands them |
| Interface | Calm mobile-first notebook | Cards with name, icon, optional accent, summary, and a one-tap log. Recognizable icons and soft cards, with less on screen than a family calendar. |
| Tracker icon | Optional, in phase 2 | Built-in picker of clear icons or emoji. Default is the tally mark. Name stays visible. Uploads stay a later idea. Moved out of later ideas so the first tracker is recognizable on its card. |
| Accent | Optional, small built-in set | Recognition on the card and icon only. Pico remains the only UI palette. Default is none. |
| Log button | Short label, still one tap | Default "+ Log". The dog card can say "+ Ate". Same quick-log and 15-minute undo as the earlier "+1" label. |
| Zero vs unlogged | Different states | A recorded zero is deliberate. No entries and no recorded zero means nothing logged. Charts leave that day blank. |
| Analysis | Overlays + delayed comparisons | Woods outings vs eating on the following 1–3 days is the test case. Patterns, not causes. Per-tracker trends sit on the tracker page, secondary to logging. |
| Ads | AdSense with a certified consent platform, after launch | No home-made consent banner. No ads on share, login, or settings |
| Start without account | Later idea | Share links cover most of the need for now |
| Install | Manifest + icons, no service worker | Verified on a phone at first deploy. Not a store app |
| First public deploy | End of phase 2 | Backups live from the start |

## Open questions

- **Share page contents**: today's summary, the log button, and today's entries only (current plan), or the recent history and streak too?
- **Undo window**: 15 minutes is the starting value. Right length?
- **Workouts**: is one number per tracker (e.g. weight for "Club mill") enough, or do weight and reps need to be recorded together? That's the trigger for fields. Relatedly, for TOI: one entry per exercise with a sets count, or one entry per set?
- **Units**: fixed per tracker (current plan), or chosen per entry?
- **Free tier**: ad-supported, limited by tracker count or history, or simply generous so $5/year is mostly a convenience purchase?
- **Does ad-free include anything else?**
- **Consent platform**: Google's "Privacy & messaging", or a third-party certified one?
- **Lag analysis**: how far beyond visual overlays should it go?
- **Deleted data in backups**: how long the privacy policy says backups keep it. The backup mechanism is in `ARCHITECTURE.md`.
