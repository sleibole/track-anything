# Immediate start and guest accounts

Specification for letting a visitor name a tracker and record an entry before adding an email, using the accounts, sessions, tracker form, and HTMX behavior that already exist.

The product sentence in `PLAN.md` under Later ideas ("Start without an account") is the origin of this work. Share links stay what they are: a way for someone else to log one tracker without an account. A guest account is the visitor's own account.

## Decisions

These are resolved from the current code and the requirements. They are not open questions.

- A guest is a normal `User` with an empty `Email`, a session, and a personal household. There is no second authentication system, no fake address, and no client-side account.
- The guest row is created only by a valid `POST` that starts tracker setup. `GET /` creates nothing.
- An existing session is reused. The start form never replaces a signed-in user.
- The tracker is created by the existing `POST /trackers` path. The homepage does not insert a tracker.
- The kind, unit, and goal come only from the setup form. The name is not parsed.
- Three identities. A **guest** has no email. A **registered** user has an email and `EmailVerifiedAt == nil`. A **verified** user has `EmailVerifiedAt` set. Guests and registered users can track. Only a verified user can invite, join a household, or start a future paid checkout.
- That invite and join rule is intentional even though current tests sign up and invite before confirmation. Those tests need permission before they are edited. See section 8. Do not weaken the rule to keep them green.
- Paid checkout is not built. When it is, the handler requires `EmailVerifiedAt != nil`. This feature does not add billing.
- No service worker exists. This feature does not add one and does not change install behavior. The footer install control stays non-blocking.
- Abandoned guests are not deleted on a schedule. A retention policy is documented below for later.
- The logged-out page keeps its current `<h1>Track Anything</h1>` and the sentence under it. That sentence is the meta description (`publicDescription` in `seo.go`). Replacing the heading would conflict with `TestProductionHomeMetadata`, `TestHomeFullPage`, and `TestHomeHTMXPartial`. The new primary action is the form label, not a new document title.

## What exists today

Logged-out `GET /` renders `templates/home.html`: the title, one sentence, Sign up, and Log in. Logged-in `GET /` renders the dashboard. `handleHome` chooses between them from the session.

Signup (`POST /signup`) creates a `User`, calls `createPersonalHousehold` in the same transaction, starts a session, and sends verification. The address is stored before it is verified. `Email` is `uniqueIndex;not null`, so two empty strings cannot be stored until that index changes. Verification and magic links live in `auth.go`. Tokens are 32 random bytes, stored as sha256. `GET` does not consume a token. `POST` consumes it with one conditional update. The first proof of a mailbox runs `claimUnverifiedEmail`, which sets `EmailVerifiedAt`, clears any password, and deletes every session. The browser that posted the link stays signed in only when that session was already for the same user. `TestVerificationLinkConfirmsOnce` requires that a logged-out browser confirming a signup link does not become signed in.

Sessions last 30 days, renew after half that life, and use an HttpOnly, SameSite=Lax cookie (`Secure` outside dev). The raw token is only in the cookie. `requireUser` sends `HX-Redirect` when an HTMX request has no session, because the header lives outside `#content`. `redirectTo` sends `HX-Location` and must not be used when the header has to change. `http.CrossOriginProtection` covers non-GET requests. Rate limiters are in-memory fixed windows created in `newApp`. Signup is 10 per hour per client IP. Turnstile runs on signup, both login forms, and password change. It does not run on ordinary tracking.

`POST /trackers` and `templates/tracker_form.html` are the only create path. Defaults are already count, Times today, the tally icon (`""`), and no accent. Kinds are `trackerKinds` in `handlers_trackers.go`. The unit and goal fields are hidden by `[data-kind-picker]` in `static/app.js`, but that binding runs once at page load and does not run again after an HTMX swap. New tracker is reached with `nav`, so this is already a hole. The setup step has to close it.

`cleanupAuth` deletes expired sessions and used or expired login and verification tokens. It does not delete users. Rewards use `withImmediate` (`BEGIN IMMEDIATE` on one connection) so a check and an insert cannot interleave. There is no offline queue and no service worker. Logging is an online POST. `render` already sends `Cache-Control: private, no-store` and `Vary: HX-Request, Cookie`. User references that are not foreign keys: `Entry.RecordedByID`, `RecordedZero.RecordedByID`, `Redemption.UserID`. Household membership is `HouseholdMember`. Home order is `TrackerOrder`. There is no billing table and no per-user preference table beyond columns on `User`.

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

Keep `Email` as a string. Empty means no address. That matches `PasswordHash`, where empty means none. `IsGuest` is `Email == ""`. `HasVerifiedEmail` is `EmailVerifiedAt != nil`. Mail helpers return an error if asked to send to an empty address.

`User.Email` uses `gorm:"not null;uniqueIndex:idx_users_email,where:email <> ''"`. Section 3b replaces any full unique index with that partial index inside one transaction. `EmailPromptDismissedAt *time.Time` is null until **Later**.

`StartAttempt` dedupes the brief retry after `POST /start`. It is not a session.

- `TokenHash` primary key, sha256 of the `start` cookie.
- `UserID *uint`, set in the same transaction as the user.
- `ExpiresAt`, ten minutes after the row is inserted.
- `ConsumedAt *time.Time`. A consumed or expired row never mints a session.

`GuestClaim` is one guest's attempt to attach an address. A new attempt and an existing-account transfer are its two outcomes.

- `GuestUserID`, `Email` (normalized), `Next` (a safe local path, often empty), `ExpiresAt`.
- `CancelledAt` when a newer attempt, logout, or a finished claim supersedes it.
- `ConsumedAt` when that attempt succeeds.

`VerificationToken` gains `Email` and `ClaimID *uint`. Empty `Email` and a nil `ClaimID` mean today's signup token, which confirms the address already stored on the user. A guest-claim token stores the proposed address and the claim id. It still cannot be presented as a login token.

`TransferIntent` is the existing-address outcome. `TransferGuestID` on a login token is not a field. The login token may point at an intent; that pointer is not permission to move data.

- `ClaimID` unique, `SourceProofHash` unique (sha256 of the `transfer` cookie).
- `SourceUserID`, `SourceSessionID` (the guest session that submitted the form).
- `DestUserID`.
- `DestVerifiedAt *time.Time`, set only when that destination user consumes the magic link that points at this intent.
- `ExpiresAt` matches the magic link, 15 minutes. `CancelledAt`, `ConsumedAt`.

`TransferReceipt` survives the guest. `SourceProofHash` is the primary key, plus `DestUserID` and `CompletedAt`. A repeated move with that same proof and that destination session returns success after the guest row is gone. Nothing else is looked up by "latest pending transfer."

`LoginToken.TransferIntentID *uint` is null for an ordinary magic link. Superseding a claim marks used only login tokens whose `TransferIntentID` is one of that guest's intents. Other login links for the destination user stay valid.

`TrackerCreate` is `RequestKey` primary key, `UserID`, `TrackerID`. It is the replay row for `POST /trackers`.

### Email index

`ensurePartialEmailIndex` runs at the end of `migrate`, inside `db.Transaction`. SQLite DDL rolls back with that transaction.

1. Read `sqlite_master` for indexes on `users`.
2. If `idx_users_email` is already `UNIQUE` on `email` with `WHERE email <> ''`, and no other unique index covers `email`, stop.
3. Otherwise `CREATE UNIQUE INDEX idx_users_email_partial ON users(email) WHERE email <> ''` first, while the old unique index still exists.
4. Drop the old full unique index only after that create succeeds.
5. `CREATE UNIQUE INDEX IF NOT EXISTS idx_users_email ON users(email) WHERE email <> ''`, then drop the temporary name.

A failure at any step rolls the transaction back, so addresses are never left without a unique index. The partial predicate keeps the current rule: stored emails are trimmed and lowercased, and two equal non-empty emails cannot exist. Empty emails are not unique. Running it again on a fresh database or an upgraded one changes nothing once the partial `idx_users_email` is in place. A test reads the SQL back from `sqlite_master`. The GORM tag uses the same name and `where` clause so `AutoMigrate` does not create a second full unique index on the next startup. A second test starts from a table that still has the old full index, runs `migrate`, and checks the resulting SQL. A third runs the replacement and forces the transaction to return an error, then checks the full unique index is still there.

### Start attempts

`GET /` for a logged-out visitor sets the cookie `start` to `newToken()`, HttpOnly, SameSite=Lax, `Secure` outside dev, path `/`, `MaxAge` ten minutes. It does not insert a row. Logged-in `GET /` does not set it.

The retry window is those ten minutes, and only while the row is unconsumed and the user is still a guest. It exists so a double submit, or a submit whose response never reached the browser, does not create a second user and can still receive a session. It is not a way back in after logout, after the email is attached, after a transfer, or after the ten minutes. Clearing the cookie on a response the browser received means that browser will not send it again. A copy of the cookie value still works until expiry or invalidation. That is the limit. There is no longer-lived recovery secret.

`POST /start` uses `withImmediate`, not "SQLite has one connection":

1. Rate-limit like signup when there is no session: 10 per hour per client IP, a limiter beside `signupLimiter`. Over the limit, re-render the homepage and create nothing. No Turnstile.
2. If `currentUser` is set, do not create a user and do not mint a session from the start cookie. Continue to setup with that account. Clear the `start` cookie.
3. Validate the name with the same trim and `maxTrackerName` rules. On failure, re-render. Do not insert an attempt.
4. In the immediate transaction, insert `StartAttempt` on the cookie hash with `ExpiresAt`. The unique primary key makes a concurrent insert fail. The loser re-reads the committed row. The winner creates the user and `createPersonalHousehold`, then sets `UserID` with `WHERE user_id IS NULL`. If that update matches zero rows, the transaction rolls back and the request re-reads. A missing cookie still creates one user, with no attempt. A lost response from that no-cookie case can duplicate; the homepage response sets the cookie, so a normal browser has it.
5. After commit, mint a session only when the row is unconsumed, unexpired, `UserID` is set, and that user still has an empty email. Otherwise clear the cookie, create nothing, and show the form again. Do not mint for a registered user even if the hash matches.
6. Clear the `start` cookie on the response that carries the new session. Leave `ConsumedAt` null so a client that never stored this response can retry with the cookie it already had.

Consume the attempt (`ConsumedAt`) on guest logout, on a successful attach, and on transfer, for every attempt of that guest. `cleanupAuth` also deletes attempts with `ExpiresAt` in the past. An expired or consumed attempt cannot mint a session. A new `GET /` after that mints a new cookie and, on the next valid post, a new guest.

Time zone uses the hidden field. The guest's "today" is that zone, same as signup. A crawler that only GETs `/` gets a cookie and no row.

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

`GET /account/email` and `POST /account/email`, both `requireUser`. A user who already has an email is sent to Settings. The form is one email field, **Continue**, a hidden `next` when the caller is preserving a join URL, and the Turnstile widget. Turnstile is justified because this post sends mail. CSRF and the existing mail and verify limiters still apply. A failed Turnstile check does not send mail.

Validate with `validEmail` and `normalizeEmail`. On failure, re-render the address and the error.

The response page is the existing “check your email” card either way. It must not say whether the address is registered.

### One claim, two outcomes

`POST /account/email` from a guest, inside `withImmediate`, cancels every unconsumed `GuestClaim` for that guest. That sets `CancelledAt` on those claims and on their transfer intents, marks their guest-claim verification tokens used, and marks used only the login tokens whose `TransferIntentID` points at those intents. Ordinary login tokens for the destination user are not touched. Then it inserts the new claim.

If no user has that email, the outcome is a verification token: `UserID` is the guest, `Email` is the proposed address, `ClaimID` is the new claim, expiry is 24 hours. `User.Email` stays empty. The mail says the link confirms the address for the account that asked to add it, works once, and expires in 24 hours, and that the recipient should ignore it if they did not ask. It does not say to open the link anyway. That sentence belongs to signup, where the address is already on a row.

If a user already has that email, the outcome is a transfer intent and a magic link for the destination user with `TransferIntentID` set. The raw source proof is a new `transfer` cookie on the guest browser: HttpOnly, SameSite=Lax, `Secure` outside dev, path `/`, `MaxAge` 15 minutes. Only its hash is stored. The emailed link does not contain it. The mail can say to confirm the move in the browser where they started. `startSession` replaces the session cookie and does not clear `transfer`, so a later destination login on this browser keeps the source proof.

While a claim is pending, the guest session keeps working. Password set stays refused until `EmailVerifiedAt` is set.

### Confirming a new address

`GET /verify/{token}` does not consume the token. Signup tokens still autosubmit. A token with `Email` set does not. The button works without JavaScript.

`POST /verify/{token}` for an empty `Email` is unchanged: `claimUnverifiedEmail`, then a new session only when the poster was already that user. A logged-out poster is not signed in. `TestVerificationLinkConfirmsOnce` depends on this.

For a token with `Email` set:

- If the current session is a different user, do not consume the token and do not change either user. The page says this link will not change the account they are signed in as.
- Otherwise one immediate transaction:
  - If the claim is cancelled, mark the token used, attach nothing, and commit. The page says the link is no longer valid.
  - If the conditional `used_at` update matches zero rows, commit nothing. Expired, reused, and unknown tokens show the existing gone page.
  - Reload the source user. The update that sets the email is `WHERE id = ? AND email = ''`. Zero rows means the address was already attached: if it is this same verified address, success; if it is any other address, leave it and do not replace it.
  - If another row already has this email, or the update hits the unique index, leave the guest unchanged, keep the token consumed, and commit. The page says this address could not be added, the trackers are still on this guest account, and **Continue** goes to `/account/email` to send a fresh link. That second submit finds the existing account and starts a transfer, which still needs a new destination login and the move button. The first add-email response still does not say the address is registered.
  - Otherwise set `Email`, `EmailVerifiedAt`, and clear `PasswordHash`. Delete that user's sessions, consume their start attempts, and cancel every other claim credential for that guest.
- If the browser's session was this guest, `startSession` so they stay signed in. A logged-out browser does not receive a session. Send them to `safeNext(claim.Next)` when that path is not `/`, otherwise `/verify/done`.

`/verify/done` keeps the words “Email verified”. For a guest claim, add that this address is now on the account that asked for it. Two concurrent posts of the same token: one conditional update wins; the other sees a used token. Two different tokens are serialized by the immediate transaction; the second sees a non-empty email and does not replace it.

### Transfer

Destination verification and the move are separate.

Consuming the magic link logs in as the destination user, using the existing first-confirmation rules, and sets `DestVerifiedAt` only when the intent is for that user, the source is still a guest, and the intent is unexpired, uncancelled, and unconsumed. It does not move data. It then redirects to `GET /account/transfer`.

That page:

- Signed in as the destination or the source guest, `transfer` cookie matches the intent, destination is verified, source session row still exists: explain the move and show **Move trackers**. **Not now** returns home and leaves both accounts.
- Destination verified but the cookie is absent (the link was opened on another device): say to go back to the browser where they started. No move button.
- Cookie present but destination not verified: say to open the email link first.
- Signed in as anyone else: say this transfer is not for this account. Do not consume the intent.

`POST /account/transfer` requires `confirm=move`. One immediate transaction checks all of the following. A failure rolls back and leaves both accounts unchanged.

1. Hash the `transfer` cookie. If a `TransferReceipt` exists for that hash and the session user is `DestUserID`, return success and write nothing. If the receipt exists for someone else, reject it. There is no lookup of the latest intent.
2. Load the intent by that hash. Reject expired, cancelled, consumed, or missing proof.
3. Require `DestVerifiedAt`. Require the session user to be the source guest or the destination user.
4. Reload the source. Require an empty email. Require `SourceSessionID` to still be a session row.
5. Move trackers from the guest's personal household onto the destination's personal household. Same tracker rows and share tokens. Append positions.
6. For any other household the guest owns: if the destination does not already own that name, reassign the owner membership. If the name collides and the guest household has no other members, move its trackers into the destination household of that name and delete the empty household. If it has other members, abort.
7. Reassign a guest membership in someone else's household only when the destination is not already a member; otherwise delete the guest membership. Guests cannot join, so this is defensive.
8. Reassign `TrackerOrder`, deleting a guest row that would duplicate the destination's primary key. Reassign `Entry.RecordedByID`, `RecordedZero.RecordedByID`, and `Redemption.UserID`. Reassign `TrackerCreate.UserID` so a replay key still points at the same tracker.
9. Insert the receipt, mark the intent and claim consumed, consume start attempts, delete the guest's sessions, verification tokens, and login tokens, cancel any other claims, and delete the guest user.
10. `RewardRequest` has no user id and stays with the tracker. The destination keeps their own time zone, password, and email. The guest's prompt timestamp dies with the user row.

Logging and tracker create also take `withImmediate` and re-check, immediately before the write, that the acting user is still a member of the tracker's household (or still owns it, for create). A transfer that commits first makes the later write fail instead of leaving a row on a deleted membership. Share-link writes have a nil recorder and skip that check.

After commit, if the session was the guest, start a destination session. `HX-Redirect` home when the header must change. Tracker ids and entry ids are unchanged. The destination login on another device never satisfies step 3 by itself, because that browser has no `transfer` cookie. The original browser still has the cookie and the guest session, and that is where **Move trackers** is pressed.

## 6. What guests may do

Guests can use the existing personal-tracking features: create and edit trackers up to the limit, log, undo, charts, history, export, archive, restore, reorder, share links, time zone, and rewards on their own cumulative trackers.

They cannot, and neither can a registered user whose email is not verified yet:

- Turn on or regenerate an invite link. `handleInviteOn` requires `EmailVerifiedAt != nil`. Otherwise it re-renders the household page, writes nothing, and explains why. A guest is offered **Add email**. A registered user is offered the existing resend on Settings. Turning an invite off stays available so a verified owner can disable a link they already created; an unverified owner has no link to disable because they cannot create one.
- Join. `handleJoinPage` and `handleJoin` require `EmailVerifiedAt != nil` before showing the join button or inserting a member. The page keeps the invite token and links to add-email or Settings with `next=/join/{token}`. After verification on that same browser, `claim.Next` sends them back to the join page. Do not join them automatically. A logged-out visitor still goes to login or signup.
- Start a paid subscription. There is no checkout yet. `ARCHITECTURE.md` records that the future handler requires `EmailVerifiedAt != nil`.

Personal tracking, including charts, export, share links, archive, and rewards on their own trackers, stays available to guests and to registered users who have not confirmed yet. Password set stays the existing verified-email check.

Creating further households stays allowed under the cap below. Inviting from them does not.

### Limits

Constants next to the other limits, not scattered:

- Guest creation: 10 per hour per client IP. Same window style as `signupLimiter`.
- `maxGuestTrackers = 20` active plus archived, counted for households they own. Past that, `handleCreateTracker` re-renders the form. The message says the limit and links to add email. Users with an email are not capped.
- `maxGuestHouseholds = 5`, including “My trackers”. Same pattern on household create.

No CAPTCHA on logging, on `GET /`, or on tracker setup. Turnstile stays on the mail and password forms, plus the new add-email form.

### Sign out

The header log-out control for a guest is a link to `GET /logout/confirm`, not an immediate post. That page shows the guest warning, **Add email**, and a real `POST /logout` button labeled so it is clear the session will end. Registered users keep the one-click logout form. `POST /logout` deletes the session, and when that user is a guest it also consumes their start attempts and cancels their claims and transfer intents, then clears the `start` and `transfer` cookies. Its success response is still a full document, because the header changes.

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
- A guest cannot read another user's tracker (404). Invite and join require `EmailVerifiedAt`. A guest and a registered unverified user are both refused, and nothing is inserted. A verified user still can. This contradicts the tests listed below; do not edit those tests without permission.
- The `start` cookie does not mint a session after logout, after the email is attached, after transfer, after `ConsumedAt`, or after ten minutes. It does not mint a session for a user who has an email. Two overlapping posts with one cookie create one user. A second post that never received the first response, still inside the window, gets a session for that same guest.
- Guest-claim verification sets the email on that user id, keeps tracker and entry ids, and does not verify before the post. An expired, reused, or superseded token does not attach. A post while a different user is signed in does not consume the token. A logged-out post attaches the email and does not sign that browser in. A second address confirmed after the first is attached does not replace it. Two concurrent confirms of one token attach once.
- If the address is registered between the mail and the confirm, the guest row and its trackers stay, the token is consumed, and the page links to a fresh add-email. The original add-email page did not say the address was registered.
- Add-email for an address that already exists does not change the guest and does not say the address is taken. The destination link without the `transfer` cookie does not move data. The cookie without `DestVerifiedAt` does not move data. A third account's session does not consume the intent. After the original browser posts **Move trackers**, the destination owns the same tracker ids and the guest user is gone. A second post with the receipt and the destination session changes nothing. A forced error inside the move leaves both users and a tracker still on the guest. The receipt, not a deleted guest row, answers the retry.
- Replacing the email index on a database that still has the full unique index leaves exactly the partial index. A forced error during that replacement leaves the full unique index in place. A fresh database already has the partial index. Two empty emails can be stored; two copies of one real address cannot.

### Existing tests that will fail until they are updated

Signup does not set `EmailVerifiedAt`. These tests then invite or join and assert the membership. They need to confirm the email before that step. Do not change them without permission.

- `sharedHouse` in `households_test.go` posts invite and join, then requires `roleMember` (`member did not join`). Every test that calls `sharedHouse` fails with it: `TestMembersCantManageHousehold`, `TestOwnerRemovesAndPromotesMembers`, `TestRemovingAMemberRotatesTheInviteLink`, `TestNonMembersGet404ForHouseholds`, `TestMoveTrackerBetweenOwnedHouseholds`, `TestLeaveHousehold`, and the `sharedHouse` callers in `trackers_test.go`, `charts_test.go`, `charts_page_test.go`, `rewards_test.go`, `cumulative_test.go`, `phase4_test.go`, `order_test.go`, `home_order_test.go`, `dashboard_list_test.go`, and `history_day_test.go`.
- `TestInviteLinkJoinFlow` asserts the invite page contains `/join/` and that signup-then-join ends on the household.
- `TestDashboardDisambiguatesDuplicateNames` and `TestOwnedHouseholdNamesStayDistinctForOnePerson` join a member before they assert labels.
- `TestTodayFollowsEachViewersStoredZone` in `trackers_test.go` invites and joins.
- `TestOverlayFollowsStoredZone` in `charts_test.go` and `TestChartsFollowStoredZone` in `charts_page_test.go` do the same.

The smallest update, once allowed, is to confirm each signup with the verification link before the invite or join post. Assertions about roles and page contents stay. `TestVerificationLinkConfirmsOnce` and the other signup verification tests must keep passing unchanged, because signup tokens still have an empty `Email`.

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
- `ARCHITECTURE.md`: the partial email index, `IsGuest`, `StartAttempt`, `GuestClaim`, `TransferIntent`, `TransferReceipt`, routes, limits, and the `HX-Redirect` on `POST /start`. Note that invite, join, and future billing require a verified email.
- `HTMX-IMPLEMENTATION.md`: the shell change on start, the prompt OOB, and the placeholder and kind-picker lifecycle.
- The suggestion list's location, the limit constants, and the retention proposal, in those docs rather than only here.

## Build order

1. Partial email index, in a transaction, and `IsGuest`. Confirm a fresh database and an upgraded one.
2. `POST /start`, `StartAttempt`, personal household, session, homepage form, and `HX-Redirect`. Homepage GET still creates nothing.
3. Minimal setup on the existing tracker form, name carried through, replay key on create.
4. Prompt, dismissal, settings copy, header link, logout confirm, and start-attempt invalidation on logout.
5. Verified-email refusal for invite and join, and the guest tracker and household caps. Stop before editing the tests in section 8.
6. One `GuestClaim` for a new address, bound verification, same-user upgrade, and the registration race.
7. Transfer intent, source cookie, destination magic link, explicit move, receipt, and reference cleanup.
8. Docs and the browser pass.

## Out of scope

Account deletion, scheduled guest deletion, Paddle, ads, a service worker, offline sync, inferring a tracker kind from the name, a second create-tracker implementation, and CAPTCHA on the homepage.
