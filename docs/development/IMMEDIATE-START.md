# Immediate start and guest accounts

Implementation plan. This file does not change the application. It records how to let a visitor name a tracker and record an entry before adding an email, using the accounts, sessions, tracker form, and HTMX behavior that already exist.

The product sentence in `PLAN.md` under Later ideas ("Start without an account") is the origin of this work. Share links stay what they are: a way for someone else to log one tracker without an account. A guest account is the visitor's own account.

## Decisions

These are resolved from the current code and the requirements. They are not open questions.

- A guest is a normal `User` with an empty `Email`, a session, and a personal household. There is no second authentication system, no fake address, and no client-side account.
- The guest row is created only by a valid `POST` that starts tracker setup. `GET /` creates nothing.
- An existing session is reused. The start form never replaces a signed-in user.
- The tracker is created by the existing `POST /trackers` path. The homepage does not insert a tracker.
- The kind, unit, and goal come only from the setup form. The name is not parsed.
- Invite and join stay available to anyone who already has an email, including an email that is not confirmed yet. Today's signup tests invite and join before verification, and signup already uses the app before the link is opened. Guests, who have no email, cannot invite or join.
- Paid checkout is not built. When it is, it requires `EmailVerifiedAt != nil`. This feature does not add billing.
- No service worker exists. This feature does not add one and does not change install behavior. The footer install control stays non-blocking.
- Abandoned guests are not deleted on a schedule. A retention policy is documented below for later.
- The logged-out page keeps its current `<h1>Track Anything</h1>` and the sentence under it. That sentence is the meta description (`publicDescription` in `seo.go`). Replacing the heading would conflict with `TestProductionHomeMetadata`, `TestHomeFullPage`, and `TestHomeHTMXPartial`. The new primary action is the form label, not a new document title.

## What exists today

Logged-out `GET /` renders `templates/home.html`: the title, one sentence, Sign up, and Log in. Logged-in `GET /` renders the dashboard. `handleHome` chooses between them from the session.

Signup (`POST /signup`) creates a `User`, calls `createPersonalHousehold` in the same transaction, starts a session, and sends verification. The address is stored before it is verified. `Email` is `uniqueIndex;not null`, so two empty strings cannot be stored until that index changes. Verification and magic links live in `auth.go`. Tokens are 32 random bytes, stored as sha256. `GET` does not consume a token. `POST` consumes it with one conditional update. The first proof of a mailbox runs `claimUnverifiedEmail`, which sets `EmailVerifiedAt`, clears any password, and deletes every session. The browser that posted the link stays signed in only when that session was already for the same user. `TestVerificationLinkConfirmsOnce` requires that a logged-out browser confirming a signup link does not become signed in.

Sessions last 30 days, renew after half that life, and use an HttpOnly, SameSite=Lax cookie (`Secure` outside dev). The raw token is only in the cookie. `requireUser` sends `HX-Redirect` when an HTMX request has no session, because the header lives outside `#content`. `redirectTo` sends `HX-Location` and must not be used when the header has to change. `http.CrossOriginProtection` covers non-GET requests. Rate limiters are in-memory fixed windows created in `newApp`. Signup is 10 per hour per client IP. Turnstile runs on signup, both login forms, and password change. It does not run on ordinary tracking.

`POST /trackers` and `templates/tracker_form.html` are the only create path. Defaults are already count, Times today, the tally icon (`""`), and no accent. Kinds are `trackerKinds` in `handlers_trackers.go`. The unit and goal fields are hidden by `[data-kind-picker]` in `static/app.js`, but that binding runs once at page load and does not run again after an HTMX swap. New tracker is reached with `nav`, so this is already a hole. The setup step has to close it.

`cleanupAuth` deletes expired sessions and used or expired login and verification tokens. It does not delete users. There is no offline queue and no service worker. Logging is an online POST. `render` already sends `Cache-Control: private, no-store` and `Vary: HX-Request, Cookie`.

## 1. Homepage

Logged-out `templates/home.html` keeps the heading and the existing sentence, then adds one form:

- Visible `<label for="tracker-name">What are you tracking?</label>`. The placeholder is not the label.
- `<input id="tracker-name" type="text" name="name" required maxlength="60" autocomplete="off">`.
- Submit button **Start tracking**.
- Supporting text: “No email needed. Start now and add an email later.”
- A **Sign in** link to `/login`. It is a normal link, not `hx-boost`, same as the header's Log in. The header keeps **Log in** and **Sign up** so the current header tests and `DESIGN.md` navigation stay true.

`action="/start"` and `method="post"`. Add `hx-post="/start"` so HTMX enhances the same form. A hidden `timezone` input uses `data-browser-timezone`, the same field signup uses. The server still falls back to `UTC` when the value is missing or not an IANA name.

### Suggestions

One Go slice, next to the homepage handler, is the only copy of the list:

1. Times my dog ate
2. Weight
3. Breast feedings
4. Bench press weight
5. Dirty diapers
6. Workouts completed
7. Glasses of water
8. Books read
9. Minutes practiced
10. Hours slept
11. Miles walked
12. Cups of coffee
13. Medications taken
14. Plants watered
15. Money saved
16. Reward points earned

The template prints the first item as the input's `placeholder`, so the form works with JavaScript off. It also prints the full list once as JSON in a `data-suggestions` attribute on the input. `static/app.js` reads that attribute. It does not contain the strings.

Placeholder behavior in `app.js`:

- The first suggestion is already showing from the server.
- Rotate about every four seconds by setting `placeholder` only. Do not write the input's value. Do not animate. Do not use a live region, so assistive technology is not told about each change.
- While the input is focused or its value is not empty, stop. When it is empty and not focused, resume from the next item.
- `prefers-reduced-motion: reduce` leaves the first suggestion in place and starts no timer. Listen for a change in that media query.
- `document.hidden` clears the timer. When the page is visible again, start it only if rotation is still allowed.
- One document-level controller, guarded so a second script run does not add listeners. `htmx:afterSettle` and `htmx:historyRestore` call the same init. Store the timer id on the input and clear it in `htmx:beforeSwap` when that input is about to leave the document, and when init finds no input. A return to the logged-out home through the boosted brand link must not stack timers.

The submitted name is the input's value. The server rejects an empty name. It never substitutes the placeholder.

Invalid name: re-render `home.html` with the typed value and the error, status 422. The HTMX config already swaps 4xx into `#content`. Keep the header logged out.

Valid name with no session: create the guest (section 3), then respond with `HX-Redirect` to the setup URL. A 303 is the non-HTMX branch. `HX-Location` is wrong here because the header must change from logged out to logged in. This is the same shell rule as login.

Valid name with a session: do not create a user. `redirectTo` the setup URL. The header is already correct.

Style the form as the existing centered card (`.auth-card` / `.form-card`): label above the field, one full-width primary button, quiet supporting text. It has to work at phone width and in light and dark mode without new colors. Pico and `theme.css` already do that.

## 2. First tracker

`GET /trackers/new` already builds `trackerForm` and renders `tracker_form.html`. Accept `name` from the query, trim it, and place it in the name field when it passes the existing name rules. Ignore a query value that does not.

For a user who owns one household, the form already hides the household picker and sends that id. Guests are in that case. Do not ask them to name a household. `createPersonalHousehold` has already created “My trackers”.

Presentation of this form when it is the first step (`Minimal` on `trackerForm`, true when the user has no trackers yet, or when `name` arrived from the start form):

- Name, prefilled, still editable.
- **What you record**, using `trackerKinds` as the radio labels. Count stays the default (`kindCount`).
- Unit and repeating goal stay the existing conditional fields. Their help text stays.
- Icon, color, button label, and summary display sit in a `<details>` labeled **More options**, closed unless a validation error concerns one of them. Defaults remain the tally, no accent, empty button label, and Times today. The inputs are in the form, so they work without JavaScript. After creation, Edit is the full form, which already exists.

Do not add a second insert path. `handleCreateTracker` and `readTrackerForm` stay the writers. A missing icon or accent that is the listed empty value continues to mean the defaults.

After a successful create, keep `redirectTo` to `/trackers/{id}`. That page is where the first entry is logged. Do not show the email prompt on create. Do not put the install dialog in the way. The footer control is unchanged and is not a gate.

`POST /trackers` needs a replay key so a double submit does not create two trackers. Follow `RewardRequest`: a hidden `request` field, `newRequestKey()`, and a small `TrackerCreate` row (`RequestKey` primary key, `UserID`, `TrackerID`) written in the same transaction as the tracker. A second post of the same key by the same user redirects to the tracker that was created and inserts nothing. A missing key keeps today's behavior, so existing create tests that omit it still pass. The setup form always sends a key. A key that already belongs to another user is rejected.

Signed-in visitors who hit the limit in section 6 see the form again with the error and the name still filled in. They are not sent back to the homepage.

## 3. Guest accounts

### Schema

Keep `Email` as a string. Empty means no address. That matches `PasswordHash`, where empty means none.

Replace the full unique index with a partial one so many guests can have an empty email, while real addresses stay unique:

```sql
CREATE UNIQUE INDEX idx_users_email ON users(email) WHERE email <> ''
```

`AutoMigrate` will not reliably swap the index. `migrate` should do it explicitly and idempotently: if the index exists and its SQL is not the partial form, drop and recreate it. Existing rows all have addresses, so the rebuild is safe. GORM's tag should match (`uniqueIndex` with `where:email <> ''`) so a later AutoMigrate does not fight the partial index.

Add `EmailPromptDismissedAt *time.Time` on `User`. Null means the prompt may show.

Add `StartNonce` (`TokenHash` primary key, `UserID`, `CreatedAt`). Only the hash of the cookie is stored.

Add `Email` on `VerificationToken` for the address a guest asked to attach. Empty means today's signup token, which confirms the address already stored on the user.

Add `TransferGuestID *uint` on `LoginToken`. Null means an ordinary magic link.

`func (u User) IsGuest() bool { return u.Email == "" }` is the check. Mail helpers return an error if asked to send to an empty address.

### Creation

`GET /` for a logged-out visitor sets a cookie named `start`. The value is `newToken()`. The cookie is HttpOnly, SameSite=Lax, `Secure` outside dev, path `/`, and a session cookie. It is not a user and not a login. Logged-in `GET /` does not set it. No other route sets it.

`POST /start`:

1. Rate-limit like signup, before other work: 10 per hour per client IP, a new limiter in `newApp` beside `signupLimiter`. Over the limit, re-render the homepage with the same kind of message signup uses. Do not create a user.
2. No Turnstile. The start form is not a mail form. Add-email is.
3. If `currentUser` is set, skip creation and continue to setup with their account.
4. Validate the name with the same trim and `maxTrackerName` rules as `readTrackerForm`. On failure, re-render. Do not create a user.
5. Read the `start` cookie. In one transaction, insert `StartNonce` on the hash. On a unique conflict, load the user already stored for that hash and do not insert another user. On a new hash, insert `User` with empty email and the posted time zone (or `UTC`), then `createPersonalHousehold`. Store that user id on the nonce.
6. A missing cookie still creates one user, under the rate limit. Browsers that loaded the page have the cookie, which is what makes a double click safe. SQLite's single connection serializes the two inserts.
7. `startSession` for that user. If this request found an existing nonce user and the browser already has a session for them, do not mint a second session.
8. Redirect as in section 1.

A crawler that only GETs `/` gets a cookie and no row. A prefetch that does not POST creates nothing.

Time zone uses the hidden field. The guest's “today” is that zone, same as signup.

### Authorization

Guests pass `requireUser` and `trackerForUser` because they are users. A tracker in someone else's household is still a 404. Guest data is not public. User ids, tracker ids, and URLs are not credentials.

CSRF stays `http.CrossOriginProtection`. The new posts are ordinary same-origin forms. Do not add a parallel token.

Session renewal is unchanged. Logout deletes that session row and clears the cookie. Guest logout goes through the confirm page in section 6.

### Email assumptions to update

Anywhere a user is shown or mailed, an empty email has to be handled. Do not invent an address.

| Place | Change |
| --- | --- |
| `sendVerificationEmail`, `sendLoginLink` | Refuse an empty address. Do not call the mailer. |
| `POST /signup`, `POST /login`, `POST /login/link` | Unchanged. `validEmail` already rejects empty. Lookup by email will not match a guest. |
| Settings heading | Guests see the guest warning from section 4 instead of “Logged in as {email}”. |
| Settings email card | Guests get the add-email form, not “Send verification email” for a blank address. |
| Household member row and its confirm text | Display “Guest” when email is empty. Guests cannot join, so this is defensive. |
| `createdByOther` | Return “Guest” when the other creator has no email, not a blank label. |
| Entry attribution, redemption email, CSV `recorded_by` | Show “Guest” for an empty email while that user row exists. A missing user stays blank, as it does now. |
| Login and verify pages | They show the address from the token's user or from the token. A guest-claim token has the proposed address on the token. |

Signup's duplicate-email error stays on signup only. The add-email form must not use it.

## 4. Email prompt

Show it only when the user is a guest, `EmailPromptDismissedAt` is null, and they have at least one `Entry` or `RecordedZero` recorded by their user id. Creating the tracker does not count. A share-link log does not count.

Copy:

- Heading: **Keep your trackers**
- Text: “Add an email so you can sign in again and use your trackers on other devices.”
- **Add email** links to the add-email form (section 5).
- **Later** posts to `POST /account/email-prompt/dismiss`, which sets `EmailPromptDismissedAt` if it is still null. A repeat is a no-op.

The prompt is a small article at the top of the dashboard and the tracker page, not a dialog, and not `aria-live`. It has a visible heading. It does not cover the log control.

Quick log and the other in-place log responses update regions out of band and do not replace `#content` (`returnToPage` / `writeInPlace`). Put an empty `#keep-trackers` element on the dashboard and tracker templates, outside the card and outside the refreshed log, trend, and history regions. When a successful log crosses the “first record” line, include the prompt as `hx-swap-oob="outerHTML:#keep-trackers"` on that response. Full page renders include it when the rule says so. A 204 refresh leaves it alone. **Later** swaps the element back to empty. Dismissal is a column, so an HTMX refresh or a new navigation does not bring it back.

**Add email** remains in the header for guests, as a normal text link beside Charts, and on Settings. The header is outside `#content`, so it appears on the full document after the start redirect. Verification and transfer that change guest status use `HX-Redirect` when the header must drop that link.

Settings, for a guest, includes:

“You’re using a guest account. If you clear your browser data or lose this session, you won’t be able to recover your trackers without adding an email.”

Do not say the trackers live only in the browser. They are rows in SQLite.

## 5. Adding an email

`GET /account/email` and `POST /account/email`, both `requireUser`. A user who already has an email is sent to Settings. The form is one email field, **Continue**, and the Turnstile widget. Turnstile is justified because this post sends mail, like signup and the magic-link form. CSRF and the existing mail and verify limiters still apply. A failed Turnstile check does not send mail.

Validate with `validEmail` and `normalizeEmail`. On failure, re-render the address and the error.

The response page is the existing “check your email” card either way. It must not say whether the address is registered.

### New address

In one transaction: delete this user's unused verification tokens, then insert a `VerificationToken` for this guest with `Email` set to the normalized address. `User.Email` stays empty. `EmailVerifiedAt` stays null. Send the confirmation mail with the usual `/verify/{token}` link.

The message is not the signup message. Signup says to open the link even if you did not sign up, because the address is already on an account someone may be using. Here the address is not on the account until the link is used. The mail says the link confirms the address for the Track Anything account that asked to add it, works once, and expires in 24 hours. If the recipient did not ask, they should ignore it. Opening it is what attaches the address.

While the token is unused, the guest session keeps working. Settings can say a confirmation was sent to that address by reading the latest unused token. That is pending, not verified. Password set stays refused until `EmailVerifiedAt` is set, which is the current rule.

`GET /verify/{token}` still does not consume the token. The confirm button shows the proposed address. Do not autosubmit a guest-claim token. A scanner that only opens the link must not attach the address. The button works without JavaScript. Signup tokens keep their current autosubmit behavior so existing verification tests stay valid.

`POST /verify/{token}` branches:

- Token has an empty `Email`: today's path, unchanged. `claimUnverifiedEmail`, then a new session only when the poster was already that user. A logged-out poster is not signed in. `TestVerificationLinkConfirmsOnce` depends on this.
- Token names an address, and the current session is a different user: do not consume the token, do not change either user, do not replace the session. The page says they are signed in on a different account, this link will not change that account, and they should open it from the browser where they started or after logging out.
- Otherwise, in one transaction: consume the token only if it is still unused and unexpired; reload the guest; if that user already has this verified address, stop as success; if some other user already has the address, leave the guest unchanged and do not treat the token as a merge; otherwise set `Email` and `EmailVerifiedAt`, clear `PasswordHash`, and delete that user's sessions. Unique conflict on the email aborts the transaction and does not attach it. Then, if the browser's session was this guest, `startSession` so they stay signed in on a new cookie. A logged-out browser does not receive a session. Send them to `/verify/done`.

`/verify/done` keeps the words “Email verified”, which the current test looks for. For a guest claim, add that this address is now on the account that asked for it, and that another device can sign in with an email link. Do not say a different account was switched.

After success, the user is not a guest. Prompts and the header **Add email** link go away on the next full document. The same user id owns the same trackers, entries, household, and settings.

Issuing a newer token invalidates older unused guest-claim tokens for that user, so a mistyped address cannot be confirmed later.

### Address that already has an account

Do not write the address onto the guest. Do not merge. Do not copy data.

Create an ordinary magic link for the existing user, with `TransferGuestID` set to the guest's id. Send it with the normal login-link mail. Show the same check-your-email page.

`GET` still does not log anyone in. `POST` still logs in as the mailbox owner, with the existing first-confirmation rules when that account was unverified. It does not move data.

After that login, if the token carried a `TransferGuestID`, redirect to `GET /account/transfer`. That page is `requireUser` and requires the current user to be the token's user, not the guest. It names the signed-in address and says the guest trackers on the account that requested this will be moved into this account only if they press the button. **Move trackers** posts. **Not now** returns home and leaves the guest account in place.

The move runs in one transaction and is safe to repeat:

1. Confirm the current user is the target and the guest still exists, still has an empty email, and still owns the data.
2. Mark a `TransferIntent` row used (`TokenHash` of a key stored when the login token was created, or the login token's own id). A conditional update means two posts cannot both move data. If it is already used, render success and write nothing.
3. Move trackers from the guest's personal household onto the target's personal household. Same tracker rows, entries, zeros, redemptions, and share token. Append their positions. Do not create a second “My trackers”.
4. If the guest owns another household, and the target does not already own that name under the usual trim and case rule, reassign the owner membership to the target and put it after the target's other households. If the name collides, move those trackers into the target's household of that name when the guest household has no other members, then delete the empty household. Guests cannot invite, so other members should not exist. If they do, abort the transfer and leave both accounts unchanged.
5. Reassign `TrackerOrder` to the target. Where the target already has a row for that tracker, delete the guest row.
6. Reassign `Entry.RecordedByID`, `RecordedZero.RecordedByID`, and `Redemption.UserID` from the guest to the target so history stays with the person who now owns it.
7. Delete the guest's sessions, verification tokens, and login tokens, then delete the guest user.
8. Commit. On any error, the guest and the target are unchanged.

Success redirects home with a short confirmation that the trackers are on this account. The guest session cookie is already gone because the magic link started the target's session. Tracker ids and entry ids are unchanged.

Typing the address never performs this. The mailbox owner has to finish the magic link and press **Move trackers**.

## 6. What guests may do

Guests can use the existing personal-tracking features: create and edit trackers up to the limit, log, undo, charts, history, export, archive, restore, reorder, share links, time zone, and rewards on their own cumulative trackers.

They cannot:

- Turn on or regenerate an invite link. `handleInviteOn` checks `IsGuest` and re-renders the household page with an explanation and **Add email**. Nothing is written. Turning an invite off is unnecessary for a guest who cannot turn one on. Leave that path alone if it is already a no-op when invites are off.
- Join. `handleJoinPage` and `handleJoin` check `IsGuest` before inserting a member. The page explains that an email is required to join a household, offers **Add email**, and preserves `next=/join/{token}` through the add-email flow. After the email is verified on that same browser, send them to the join page. Do not join them automatically. A logged-out visitor still goes to login or signup, which is the current `requireUser` behavior.
- Start a paid subscription. There is no checkout yet. Note the `EmailVerifiedAt` check in `ARCHITECTURE.md` next to the phase 5 billing notes so it is not forgotten. Do not build Paddle.

Creating further households stays allowed under the cap below. Inviting from them does not.

### Limits

Constants next to the other limits, not scattered:

- Guest creation: 10 per hour per client IP. Same window style as `signupLimiter`.
- `maxGuestTrackers = 20` active plus archived, counted for households they own. Past that, `handleCreateTracker` re-renders the form. The message says the limit and links to add email. Users with an email are not capped.
- `maxGuestHouseholds = 5`, including “My trackers”. Same pattern on household create.

No CAPTCHA on logging, on `GET /`, or on tracker setup. Turnstile stays on the mail and password forms, plus the new add-email form.

### Sign out

The header log-out control for a guest is a link to `GET /logout/confirm`, not an immediate post. That page shows the guest warning, **Add email**, and a real `POST /logout` button labeled so it is clear the session will end. Registered users keep the one-click logout form. `POST /logout` itself stays a normal form. Its success response is still a full document, because the header changes.

### Retention

Do not delete guests in this feature. `cleanupAuth` stays limited to expired sessions and spent tokens.

Proposed policy, for a later change, not for this one:

- A guest is abandoned only when they have no email, their newest session is expired, and they have had no entry and no recorded zero for 90 days. A guest with a live or recently renewed session is active even if they have not logged today.
- Never apply that rule to a user who has an email, verified or not.
- Deletion has to be the same real delete as phase 5 account deletion, including households they alone own, their trackers, entries, and tokens. Do not build that delete here.
- Until then, expired session cookies simply fail `loadUser` and the browser is logged out. The rows remain. The guest who still has the cookie keeps renewing it by using the app, for the same 30 days as anyone else.

## 7. HTMX and the installed app

Follow `docs/development/HTMX.md` and `HTMX-IMPLEMENTATION.md`.

- Start, setup, add email, dismiss, transfer, and logout confirm are normal forms with `action` and `method`. HTMX attributes are additive.
- Validation re-renders the same form with the entered values and a 422. The config swaps that status.
- Back and forward use the history element. Setup and the tracker page are real URLs. The start redirect is a document load, so it is a normal history entry.
- Placeholder timers and the kind-picker listener initialize on first load, `htmx:afterSettle`, and `htmx:historyRestore`, and they remove themselves before a swap drops their nodes. Use a data flag so one element is not bound twice. The kind picker needs this anyway: `nav` to `/trackers/new` swaps the form in after `app.js` has already run.
- The time-zone hidden field has the same hole. Set `data-browser-timezone` from the same settle path. The server fallback stays `UTC`.
- Authenticated HTML, including a guest's pages, stays `private, no-store` with `Vary: HX-Request, Cookie`. The logged-out homepage stays the only indexable page, and only in production, and only when there is no session. `POST /start` is not indexable. Do not cache it.
- There is no service worker and no offline entry queue. Do not add a cache to make this feature work. When the later offline queue is built, each queued row has to store the user id of the session that queued it. Logout, a new guest session, email claim, and transfer must not POST those rows into a different user. Drop or hold them when the current session's user id does not match. Share-link queues stay tied to the share token, not to a user. That is a requirement on the future queue, not a queue to build now.

`POST /start` that creates a session uses `HX-Redirect`. Document that beside login in `HTMX-IMPLEMENTATION.md`. The in-place log path does not change except for the optional prompt OOB.

## 8. Tests and browser checks

Do not edit existing tests unless the behavior change is intentional and permission is given. Run the current suite before editing so a failure can be compared.

These existing tests should still pass without edits if the homepage keeps its heading, description sentence, and header links:

- `TestHomeFullPage` and `TestHomeHTMXPartial` look for “Track how often anything happens” and, on the full page, `href="/signup"`.
- `TestProductionHomeMetadata` also looks for `<h1>Track Anything</h1>`, `href="/login"`, `href="/signup"`, and the description meta tag.
- `TestInAppLinksBoostAndShellLinksDoNot` requires logged-out `/login` and `/signup` to be present and not boosted.

If the heading or the description sentence is changed anyway, those assertions are the ones that would need permission. Do not weaken them to hide a mistake.

`TestVerificationLinkConfirmsOnce`, `TestVerificationLinkExpires`, `TestVerificationTokenIsNotALoginToken`, and the signup tests must keep their current outcomes. Guest-claim behavior is a branch for tokens that store a proposed address. Signup tokens leave `Email` on the token empty.

New tests, in the standard library style, with `newTestApp` and `memMailer`:

- `GET /` creates no user and no session. A second GET still does not.
- `POST /start` with an empty or too-long name creates no user and shows the error and the typed value.
- A valid post creates one user with an empty email, one “My trackers” household they own, and a session. Repeating the post with the same `start` cookie does not create a second user. A different cookie, after the first session is cleared, can create another guest, subject to the rate limit.
- A signed-in signup user and a signed-in guest who post the form keep their user id and gain no extra user.
- The name from the start form is on the setup form. A rejected setup post still shows it. The created tracker's name is what was submitted, not a suggestion they did not type.
- A guest cannot read another user's tracker (404) and cannot join or invite. An unverified signup user still can, matching current tests.
- Guest-claim verification sets the email on that user id, keeps tracker and entry ids, and does not verify before the post. An expired or reused token does not attach. A post while a different user is signed in does not consume the token and does not change either account. A logged-out post attaches the email and does not sign that browser in.
- Add-email for an address that already exists does not change the guest's rows and does not tell the guest the address is taken. After the magic link and the explicit move, the target owns the same tracker ids and the guest user is gone. A second move post does not duplicate trackers. The move post without the target's session does not transfer.

Run `gofmt` on changed Go files, `go vet ./...`, and `make test`.

Browser, on a named agent server from `make dev-agent`, not on port 8080:

- Logged-out home, start, setup, create, first log, prompt appears.
- **Later** hides it and a refresh or HTMX navigation leaves it hidden. **Add email** is still in the header and on Settings.
- Add a new email, open the link in the same browser, land signed in with the data, prompt gone.
- Add an email that already exists, complete the magic link as that account, and confirm the move only happens after the button.
- Phone width, keyboard through the form, reduced motion leaving one placeholder, light and dark.
- Boosted navigation away from and back to the logged-out home does not start a second placeholder timer.
- Log out as a guest shows the warning and **Add email** before the session ends.
- Install footer still opens and is not required before the first log.

There is no offline queue to exercise. Say that in the completion note.

## 9. Documentation to update while building

- `PLAN.md`: describe guest start under the current product, and remove or mark the Later ideas bullet. State the invite, join, and billing limits, and point at the retention proposal.
- `DESIGN.md`: the logged-out home form, the prompt, guest settings copy, and guest logout. Header navigation for a guest adds **Add email**. Logged-out header text stays **Log in** and **Sign up**.
- `ARCHITECTURE.md`: the partial email index, `IsGuest`, `StartNonce`, verification token address, transfer, routes, limits, and the `HX-Redirect` on `POST /start`. Note that billing must require a verified email.
- `HTMX-IMPLEMENTATION.md`: the shell change on start, the prompt OOB, and the placeholder and kind-picker lifecycle.
- The suggestion list's location, the limit constants, and the retention proposal, in those docs rather than only here.

## Build order

1. Partial email index and `IsGuest`, with no behavior change for users who have an email. Run the suite.
2. `POST /start`, nonce, personal household, session, homepage form, and `HX-Redirect`. Homepage GET still creates nothing.
3. Minimal setup on the existing tracker form, name carried through, replay key on create.
4. Prompt, dismissal, settings copy, header link, logout confirm.
5. Guest invite and join refusal, and the tracker and household caps.
6. Add-email for a new address, bound verification, same-user upgrade.
7. Existing-address magic link and the explicit transfer.
8. Docs and the browser pass.

## Out of scope

Account deletion, scheduled guest deletion, Paddle, ads, a service worker, offline sync, inferring a tracker kind from the name, a second create-tracker implementation, and CAPTCHA on the homepage.
