# HTMX

A practical guide for people and coding agents building server-rendered sites with [htmx](https://htmx.org/) 2.x. It is opinionated. It is not a second copy of the htmx documentation, and it is not a description of any one application.

Library behavior below was checked against htmx **2.0.11** and the documentation published for that version. Recommendations are marked as such. Where 2.0.11 and the docs disagree, the installed library wins, and the disagreement is called out.

Copy this file into another Go project as-is. Delete the last section if that project is not this repository.

## Principles

htmx is a way to progressively enhance a normal website. The server still owns application state and HTML. The browser still owns navigation, forms, history, and focus, except where an attribute deliberately takes over.

- Prefer server-rendered HTML. A response is HTML the browser can show, not JSON the client must interpret.
- Use htmx where a full document load is the wrong granularity: swapping a region, refreshing a list, posting a form without leaving the page. Do not use it to assemble a client-side router.
- Prefer an HTML attribute over a script. Add JavaScript when htmx and the browser cannot express the behavior.
- Make the behavior visible in the markup. A reader should be able to see what is requested, what is replaced, and what happens to the URL.
- Keep native behavior. Links open, forms submit, modified clicks do what the browser does, and the page works with JavaScript disabled whenever that is reasonable.
- Favor a boring solution. A duplicated attribute is easier to debug than a clever inherited default that surprises the next request.
- Treat every preference as a default, not a law. The exceptions are in the sections below.

## 1. Request and response architecture

Decide the response shape from the kind of request. Do not pick it from a single header and reuse it for every case.

| Request | What the browser is doing | What to return |
| --- | --- | --- |
| Ordinary navigation or form post | Loading a document | A complete HTML document |
| Boosted navigation whose target is `body` | Replacing the body in place | A complete HTML document. htmx keeps the body and the title, and discards `<head>` |
| Boosted or explicit request whose target is a region | Replacing that region | The HTML that belongs in that region, or a complete document plus `hx-select` / `HX-Reselect` that picks the region out |
| History restore after a cache miss | Filling the history element | HTML that matches the history element. See [History](#6-history-caching-and-security) |
| Poll or other background refresh | Updating a widget | A small fragment, or `204` when nothing changed |

### Headers that matter

htmx sends these on the requests it issues. A normal browser navigation sends none of them.

| Header | 2.0.11 meaning |
| --- | --- |
| `HX-Request: true` | The request came from htmx. On a history cache miss this is also sent, because `historyRestoreAsHxRequest` defaults to `true` |
| `HX-Boosted: true` | The element is boosted. Boosted requests also send `HX-Request` |
| `HX-Target` | The `id` of the swap target, when it has one. Absence means you cannot infer the target |
| `HX-History-Restore-Request: true` | A history cache miss. Sent even if you turn `historyRestoreAsHxRequest` off |
| `HX-Current-URL` | The URL the browser is on when the request starts |
| `HX-Trigger` / `HX-Trigger-Name` | `id` and `name` of the element that triggered the request |

`HX-Boosted` is how you tell a boosted full-page request from an explicit fragment request. `HX-Request` alone is not enough in 2.0.11, because history restores send it too.

### Selecting a response in Go

Branch in one place, and test each branch. `Vary: HX-Request` matters if any HTTP cache might store the response: a full document and a fragment share a URL, and without `Vary` a cache can serve the wrong one later.

```go
// Match these to the markup. They are not something the request can tell you.
const (
    boostReplacesBody    = false // true only when boosted links swap <body>
    historyElementIsBody = false // true when no element has hx-history-elt
)

func render(w http.ResponseWriter, r *http.Request, full, fragment []byte) {
    w.Header().Set("Vary", "HX-Request")
    if fragmentResponse(r) {
        w.Header().Set("Content-Type", "text/html; charset=utf-8")
        _, _ = w.Write(fragment)
        return
    }
    w.Header().Set("Content-Type", "text/html; charset=utf-8")
    _, _ = w.Write(full)
}

func fragmentResponse(r *http.Request) bool {
    if r.Header.Get("HX-Request") != "true" {
        return false
    }
    // A miss does not send HX-Boosted. Branch on it separately.
    if r.Header.Get("HX-History-Restore-Request") == "true" && historyElementIsBody {
        return false
    }
    if r.Header.Get("HX-Boosted") == "true" && boostReplacesBody {
        return false
    }
    return true
}
```

A history restore does not send `HX-Boosted`, so a helper that only special-cases boost will return a fragment for a miss that was supposed to refill `body`.

A fragment that should update the document title includes a `<title>` as a direct child of the fragment, not wrapped in another element. htmx lifts that title and removes it before inserting. In a full document, htmx uses the document title. `<head>` is stripped and never swapped, so a title that exists only in `<head>` still updates the document when the response is a full page, but scripts and styles in `<head>` do not run again.

### Status codes

Defaults in 2.0.11:

- `204` is success and is not swapped. Response headers are still processed, so `204` plus `HX-Location` or `HX-Trigger` works.
- Other `2xx` and `3xx` responses are swapped.
- `4xx` and `5xx` are not swapped and are treated as errors (`htmx:responseError`).

A validation error that must re-render the form should be `200` with the form HTML, unless you deliberately configure `htmx.config.responseHandling` (or handle `htmx:beforeSwap`) to swap a specific `4xx`. Do that in one place and test it. A silent `422` looks like a dead button.

The browser follows a `303` or `302` inside `XMLHttpRequest` and hides that response from htmx. `HX-Location`, `HX-Redirect`, `HX-Push-Url`, and `HX-Retarget` on the redirect itself are discarded. Put those headers on a response htmx actually sees: the final response, or a `200`/`204` that is not a redirect. For a non-htmx client, keep the ordinary `303`.

```go
func seeOther(w http.ResponseWriter, r *http.Request, path string) {
    if r.Header.Get("HX-Request") != "true" {
        http.Redirect(w, r, path, http.StatusSeeOther)
        return
    }
    // Non-redirect so htmx can read the header. Push defaults to true.
    w.Header().Set("HX-Location", `{"path":"/items/1","target":"#content","swap":"innerHTML"}`)
    w.WriteHeader(http.StatusNoContent)
}
```

Build that JSON with `encoding/json` when any part of it is not a constant. htmx treats a missing `push` as `"true"`. The string `"false"` suppresses the history entry. A JSON boolean `false` also suppresses it.

## 2. Navigation and page swaps

### What boost does

`hx-boost="true"` turns same-origin anchors and forms into htmx requests and leaves the real `href`, `action`, and `method` in place. Without JavaScript, the browser navigates or submits as usual.

In 2.0.11 a boosted element:

- Targets `body` and uses `innerHTML`, unless `hx-target` or `hx-swap` says otherwise.
- Pushes a history entry. The URL it pushes is the final response URL when `XMLHttpRequest.responseURL` is available, otherwise the request path. Set `hx-push-url="false"` to keep the current URL.
- Scrolls the target to the top (`scrollIntoViewOnBoost` defaults to `true`), unless `hx-swap` already has a `show` modifier. `show:none` opts out. `show:window:top` scrolls the window to the top.
- Is inherited. A boost on a parent applies to descendant links and forms. `hx-boost="false"` on a child turns it off.

Boost does not apply to a different host, an `href` that starts with `#`, or an anchor whose `target` is set to something other than empty or `_self`. Ctrl-click and Cmd-click are ignored by htmx, so the browser opens a new tab. Middle-click is an `auxclick`, which this version does not listen for. **Shift-click is not special-cased in 2.0.11.** htmx calls `preventDefault()` and follows the link in the same tab. Do not promise "modified clicks always escape boost" on this version.

### Do not boost the body by default

Boosting `<body>` is a reasonable choice when every in-app page is a full document, the chrome is inside the body and may change, and you are willing to replace the whole body on every click. It is a bad default when:

- The header, navigation, or signed-in state lives outside the thing you actually wanted to swap. The default target is `body`, so the response replaces all of it.
- Login, logout, or other documents that must change the shell are descendants of the boosted element. A fragment response then destroys the shell, and a full-page response is swapped into a target that was not ready for it.
- Some links must stay real navigations: file downloads, `Content-Disposition: attachment`, logout, or anything that should replace the document.

Two workable alternatives:

1. Boost a container that wraps only the links that should be enhanced, and return full documents. Use `hx-select` if the swap target is not the body.
2. Put `hx-boost` on each in-app anchor, with an explicit `hx-target` and `hx-swap`. Leave auth, downloads, and shell-changing actions as plain links and forms.

Option 2 is more verbose and harder to get wrong. Prefer it when the shell is stable and only the main region changes. Prefer option 1 when most of the page, including chrome, should update together.

A header link is outside `<main>`. It does not inherit `hx-target` from `<main>`. Give it a target, or it inherits the boost default and replaces `body`.

### Targets, swaps, and selection

- `hx-target` is where the main response goes. `this`, `closest …`, `next …`, `previous …`, and `find …` avoid inventing ids. An id is still clearer for a page region.
- `hx-swap` defaults to `innerHTML`, which keeps the target element. `outerHTML` replaces the element itself. After `outerHTML`, do not expect the old node, its listeners, or its id to still be there.
- Inheritance is real. `hx-target` and `hx-swap` on a parent apply to every htmx child that does not set its own. A page-level `hx-swap="innerHTML show:none"` is useful for forms and polls that must not scroll, and surprising for a link that should scroll to the top. Override on the element that differs. `hx-disinherit` or `unset` is the escape hatch.
- `hx-select` picks part of a full document before the swap. Use it when one URL must keep returning a complete page (bookmarks, non-JS, boost-to-body) and a region request needs a piece of it. `HX-Reselect` is the server-side override.

### URL, title, and stale chrome

The address bar updates when the request pushes or replaces history: boost does, `hx-push-url` does, and `HX-Push-Url`, `HX-Replace-Url`, and `HX-Location` do. An `hx-get` or `hx-post` does not, unless you ask. If the path changed and the bar did not, the next refresh loads the old URL.

The document title updates from a `<title>` in the response unless `ignoreTitle` is set. History stores that title with the snapshot.

Anything outside the swap target survives. That is the point of a region swap, and it is how a header goes stale: the user logs out, or their name changes, and the header is not in the target. When the shell must change, do a real navigation (`HX-Redirect` or `HX-Refresh`), do not swap a fragment under the old chrome. `HX-Redirect` assigns `location.href`. Use it for that case only. A normal in-app move is `HX-Location` or a boosted link.

## 3. Forms and progressive enhancement

A form that works without htmx has `method` and `action` (and `enctype` when it uploads). htmx adds `hx-post` or `hx-boost`; it does not replace the native attributes. `hx-post="/save"` with no `action` does nothing when JavaScript is off.

Match the verb to the meaning. `GET` is a query and may be bookmarked. `POST` changes state. htmx will happily `hx-get` something that deletes a row. Do not.

### Validation and errors

On a real submit, the browser validates before it fires `submit`, so the native message still shows and htmx never sees an invalid form. htmx also refuses to send a form request when `checkValidity()` fails. `htmx.config.reportValidityOfForms` defaults to `false` in 2.0.11, which matters for requests that are not native submits (`hx-validate`, a button that is not `type="submit"`). Turn it on if those requests should focus the invalid field. The current documentation notes that a future htmx 4 reports validity by default; do not assume that on 2.x. Re-check everything on the server.

Return the form again, with the errors next to the fields, as a `200` fragment or document. Do not rely on a `4xx` unless response handling is configured to swap it. Keep the entered values. Move nothing else.

### Redirect after POST

The Post/Redirect/Get pattern still matters for the non-JS document load: `303` to the new URL.

For an htmx submit, pick from the same three outcomes. Do not hide them in one helper.

1. **The URL is already correct.** A `303` back to the current page is fine. XHR follows it, htmx swaps the final HTML, and nothing is pushed. A boosted form will push `responseURL`, which is that same page.
2. **The URL must change and the shell stays.** Respond `204` (or `200`) with `HX-Location`, not `303`. The `303` headers never reach htmx. A non-boosted `hx-post` does not update the bar from `responseURL`.
3. **The shell must change** (login, logout, session expired). `HX-Redirect` to a full document. A `303` followed inside XHR would insert the new page under the old header.

A boosted form is closer to a normal submit: htmx records `responseURL` and pushes it. That covers a `303` only when the final URL is the one you want in the bar and the body you get back is the right shape for the boost target. If the boost target is a region, the final full document will be poured into that region unless you select.

### Search, debounce, and overlap

A search box belongs to a `GET` form so Enter submits it. Incremental search is an addition:

```html
<form action="/search" method="get">
  <input type="search" name="q"
         hx-get="/search"
         hx-trigger="keyup changed delay:300ms, search"
         hx-target="#results"
         hx-sync="closest form:replace"
         hx-include="closest form">
  <button>Search</button>
</form>
```

`changed` skips no-op keyups. `delay` waits for a pause. `search` covers the clear button on `type="search"`. `hx-include` matters because a `GET` from an input does not submit the rest of the form by itself. `hx-sync="…:replace"` drops the in-flight request when a new one starts, so a slow response cannot overwrite a newer one. `abort` is the other useful strategy: the form submit cancels the keyup request (`closest form:abort` on the input).

`hx-sync` strategies are `drop`, `abort`, `replace`, and `queue`. Default when the strategy is omitted is `drop`.

### Uploads, double submits, and CSRF

Set `enctype="multipart/form-data"` on the form. htmx also honors `hx-encoding="multipart/form-data"`. Either one makes it send `FormData`, which is what carries the file. Do not set `Content-Type` yourself; the browser has to add the boundary. Test with a real multipart body, not a urlencoded stand-in.

`hx-disabled-elt="this"` on the submit button, or `hx-indicator`, covers the wait. htmx reads form values before it disables controls for the request, so disabling the button does not drop the payload. Controls that were already disabled are omitted, as in a normal form. `hx-sync="this:replace"` is a second lock if a double click would otherwise start two posts.

CSRF is a server check. A token in a hidden field rides along with the form for both normal and htmx submits, which is the progressive-enhancement path. `hx-headers` on `<body>` adds a header to htmx requests only, and it goes stale if the token rotates and the body is not part of the swap. Prefer the hidden field. `SameSite` cookies are a complement, not a substitute.

### Fragment or new page

Update a fragment when the user is staying in the same task: search results, a row, an inline error, "saved" on the current screen. Navigate when the destination is a different place they might bookmark, share, or reload: a created resource, a deleted resource's parent, a change of account. If you swap the new page in and forget the URL, reload will bring the old one back.

## 4. Targeted updates and partials

A partial is the HTML for one target, not a trimmed document. It should be understandable next to the handler that returns it: the markup for `#results` is the results list, not a page with the list buried inside.

- `innerHTML` refreshes the inside of a stable element. Use it for regions that other attributes point at (`hx-target`, `hx-history-elt`, a poll).
- `outerHTML` replaces a row or a card that is the request source. The replacement must include everything the next interaction needs.
- `beforeend` appends. Use it for "load more", and return only the new items. Returning the whole list appends a second copy.
- `hx-swap-oob` updates an element by id on the side, then the main target gets the rest. The oob element must already exist. Table rows and table cells often need a `<template>` wrapper or the parser will drop them. Nested oob swaps are on by default (`allowNestedOobSwaps`).
- `hx-swap="none"` runs oob swaps and response headers without touching the main target.

### Polling

`hx-trigger="every 30s"` refetches on a timer. The element that polls is usually the element that gets replaced, so the timer comes back with the response. Poll only while the data can change and the user can see it. A hidden tab, a focused field, and an open dialog are reasons to skip. `hx-trigger="every 30s [document.visibilityState === 'visible']"` is the kind of filter that belongs in the attribute, not in a scheduler you invent.

Abort a poll that would land on top of a user action (`htmx:abort`, or `hx-sync` against the form). `204` means "leave the DOM alone". HTTP `286` still cancels the poller in 2.0.11 when the response is swapped; prefer removing the polling element or returning `204` over a magic status.

### Response headers that retarget

| Header | Use when | Avoid when |
| --- | --- | --- |
| `HX-Retarget` | One response must land somewhere other than the request's target, such as an error that replaces a different node | The target is known up front. Put `hx-target` on the element |
| `HX-Reswap` | The same response occasionally needs `outerHTML` or `none` | You can say it in `hx-swap` |
| `HX-Reselect` | The client asked for a document and the server must point at the part that applies | The response is already the fragment |
| `HX-Push-Url` / `HX-Replace-Url` | A fragment update should still change the bar, such as a search query or a selected tab | The user did not navigate. Pushing every autosave clutters history |
| `HX-Location` | After a write, move to another URL inside the current shell | The shell itself is wrong |
| `HX-Redirect` | The next page must be a new document | An ordinary page change. It throws away the in-place update and looks like a reload |
| `HX-Refresh: true` | You truly want `location.reload()` | You know the URL to send them to. Prefer `HX-Redirect` |

These headers are a server-side escape when the client could not know the outcome. They are not a second routing layer. If every response retargets, the markup is lying.

## 5. JavaScript integration

Reach for JavaScript after the attribute cannot say it: a drawing library, a drag gesture, a widget that mutates its node, a third-party script that must be present.

### Delegation and one registration

Listen on `document` or another node that is not swapped, and select the target inside the handler. A listener bound to a row dies when the row is replaced, or it piles up if you bind it again on every settle.

If a script tag is inside swapped content, htmx runs it (`allowScriptTags` defaults to `true`) by cloning the element. A history restore can run it again, because the snapshot includes the tag that remains in the DOM. Guard with a flag on `window` or check that the listener is not already there. Do not build a component registry whose only job is to emulate this.

`htmx.onLoad` (the `htmx:load` event) is the supported "new content arrived" hook. `htmx:afterSettle` runs after the settle delay, which is the right time to measure layout. `htmx:historyRestore` runs when back/forward puts a snapshot or a miss response in place; widgets that cannot survive being frozen in HTML need to be torn down and created again there. `htmx:beforeHistorySave` is where you undo a library's DOM mutations so the snapshot stays clean.

### What not to do

- Do not initialize only in `DOMContentLoaded`. The next page arrives without that event.
- Do not depend on a `<script>` in `<head>` of a swapped response. The head is removed before the swap.
- Do not keep a parallel client store of the HTML the server just rendered. If a library needs a handle (a chart instance, a widget object), keep it on the element and destroy it before the node goes away.
- Do not write a page router, a swap lifecycle, or a pub/sub bus around htmx. Events htmx already fires are enough.

Global assets (htmx itself, the site script, CSS) belong in the document that is not swapped. Page-specific behavior can live in the fragment if it is idempotent and the file is small. A large library used on one screen can be added by the site script when it sees the marker element, so the first full page and a later swap share one path.

## 6. History, caching, and security

htmx history is a tab-session snapshot of HTML. It is not the browser's HTTP cache, and it is not `Cache-Control`.

On a history push, htmx stores the history element's `innerHTML`, the document title, and `scrollY` in `sessionStorage` under `htmx-history-cache`. The default cap is 10 entries. The 2.0.11 documentation still says `localStorage` in the history and security chapters. The 2.0.11 library uses `sessionStorage`. Entries die with the tab, not when the user logs out.

The history element is `body` unless some element has `hx-history-elt`. That element has to exist on every page that participates, because restore writes into it. `innerHTML` means the element stays and its children change. A cache hit does not ask the server.

A cache miss requests the URL with `HX-History-Restore-Request: true`. In 2.0.11 it also sends `HX-Request: true`, because `historyRestoreAsHxRequest` defaults to `true`. The response is swapped into the history element with `innerHTML`. It is not a document load unless `refreshOnHistoryMiss` is `true` (default `false`), in which case htmx calls `location.reload()`.

The docs say to set `historyRestoreAsHxRequest` to `false` whenever `HX-Request` means "send a fragment", and they describe the miss as needing "the entire page". That advice fits a history element of `body`: the miss must be a full document, or the body is replaced by a fragment and the shell vanishes. It does not fit a history element that is the content region: the miss must be the fragment that belongs in that region, and a full document would be inserted inside it. Pick one model and test a miss, not only a cache hit.

`hx-history="false"` on any element in the document skips the snapshot for that save (2.0.11 checks the whole document, not just that subtree). Back/forward then misses and asks the server. Use it for pages whose HTML should not sit in `sessionStorage`. The URL still enters browser history, which is what you want for the back button. Setting `historyCacheSize` to `0` turns snapshots off globally; that is a blunter choice.

### Authentication

Authorize every request on the server, including a history restore. A cache hit never reaches the server, so authorization cannot fix a snapshot that is already on screen.

After logout, back can restore a snapshot of a signed-in page into the current document. The header the user just logged out of, or the signed-in main region, reappears from `sessionStorage` with no request. Mitigations, from lightest to strongest:

- A real document load for login and logout, so the shell matches the session at least on that navigation.
- `hx-history="false"` on pages that contain private content, so back is a miss and the server can redirect to login.
- `HX-Redirect` when an htmx request has no session, so the login document replaces the shell instead of being swapped inside it.

Do not "fix" this by disabling browser history. People use back. Make the restore either harmless or revalidated.

### HTTP caching

If a URL returns different HTML for `HX-Request` and for a normal navigation, send `Vary: HX-Request`. Otherwise a shared or browser HTTP cache can store the fragment and hand it out as the document, or the reverse. `ETag` values for those two bodies must differ. `getCacheBusterParam` is a fallback that dirties the query string; `Vary` is the actual contract. Neither one controls the htmx history snapshot.

### XSS

htmx makes HTML more powerful: an injected `hx-get` runs. Escape untrusted text in the template. If you must render a subset of user HTML, allow a list of tags and attributes, and do not allow `hx-*`, `data-hx-*`, or inline event handlers through. `hx-disable` on a wrapper is a second line of defense, not a sanitizer. `allowScriptTags` defaults to `true`, so a swapped `<script>` runs. Turn it off if your content model does not include running server-sent scripts, and know that you then need another way to wake third-party widgets.

## 7. Accessibility and browser behavior

Avoiding a full reload is not the goal. The goal is a page that still behaves like a page.

- Use real links, buttons, labels, and headings. An `a` with an `href` can be opened, copied, and activated from the keyboard. A `div` with `hx-get` cannot, unless you rebuild all of that.
- Do not break Enter. A text input inside a form submits it. A control that should not submit needs `type="button"`.
- htmx focuses `[autofocus]` in swapped content and otherwise leaves focus alone. `defaultFocusScroll` defaults to `false`. After `innerHTML`, a focused control inside the old contents is gone and focus drops to the document. For an in-place field update, do not steal focus. For a page-sized swap, putting `tabindex="-1"` and `autofocus` on the main heading is a reasonable way to give keyboard and screen-reader users a place to land. It is optional, and it is not a focus-management framework.
- htmx does not announce swaps. If a region updates without a navigation, give it `aria-live="polite"` and include the new text in the HTML, or set `aria-busy="true"` on the target for the duration of the request. A live region that fires on every poll is worse than none. Prefer the server-rendered sentence ("Saved", "3 results") over a script that speaks.
- The request indicator class is `htmx-request`. The included indicator CSS shows `.htmx-indicator` only while that class is present. An indicator still needs an accessible name if it is the only sign that something is happening.
- Pair errors with the fields (`aria-describedby` or the error text in the label's scope). A banner nobody focused is easy to miss.
- Scroll on purpose. Page changes usually want the top (`show:window:top` or the boost default). A form error, a poll, and a log action usually want `show:none`. Boost will scroll unless you say otherwise.
- Preserve new-tab behavior for ordinary links. In 2.0.11 that is reliably Ctrl/Cmd-click, middle-click, and `target="_blank"`, not Shift-click.

## 8. Performance and reliability

Do less. A region swap is already the optimization.

- Poll only what can change, and not while the document is hidden or the user is editing.
- Debounce input. Sync requests so an older response cannot win.
- Keep fragments to the region that changed. Swapping the whole page on a keystroke costs the server a full render and the browser a full parse.
- Disable the control that would start a duplicate write. Do not add a queue.
- Rendering HTML on the server is the design. Cache expensive fragments with normal HTTP rules when a profile says you should, not before.
- A network error fires `htmx:sendError`. A timeout fires `htmx:timeout` (`htmx.config.timeout` defaults to `0`, meaning no timeout). htmx does not retry. Retrying a `POST` by default is how you double-charge. Show the failure in the region the user was looking at and let them submit again.
- `4xx`/`5xx` do not swap unless you opt in. If the user gets silence, the status code is the first thing to check.
- Do not replace a large subtree to change one number. Also do not build a diffing engine to avoid it. Return the small target.

## 9. Testing and debugging

Split tests by what can lie.

**HTTP tests** can assert status, headers, and body shape. Do that for every branch:

- No htmx headers: a full document (`<!doctype html>` or the layout marker you always emit), no fragment-only payload.
- `HX-Request: true` for a region endpoint: the fragment, the title when one is required, and no second copy of the shell.
- `HX-Boosted: true` when boost targets `body`: a full document.
- `HX-History-Restore-Request: true` together with whatever `HX-Request` your config sends: the body that the history element can accept.
- A non-htmx `POST`: `303` and `Location`.
- An in-app htmx `POST` that changes path: the htmx location header on a non-redirect status, and no `HX-Redirect`.
- An htmx `POST` that must replace the document: `HX-Redirect`, and the non-htmx equivalent still `303`.
- A file upload: a real `multipart/form-data` body with a part, not a text field named `file`.
- An unauthenticated htmx request to a private page: the response that loads the login document, not a login fragment under the signed-in shell.
- `Vary` if both shapes share a URL.

**Browser tests** are required when the bug is in the DOM or the history stack. An HTTP test cannot see them.

- Click an in-app link. The URL and title change. A sentinel set on `window` before the click is still there (`window.__nav = 1` in the console, or the same from the test driver). `performance.getEntriesByType("navigation")` still reports the original document load. If the sentinel is gone, the browser loaded a document.
- The shell node you intended to keep is the same object (`document.body`, the header, the history element). The history element's children changed.
- Ctrl/Cmd-click still opens a tab, for a boosted link.
- Submit with the keyboard. Enter in a text field submits the form you think it submits.
- Back through several htmx pages, then Forward. URL, title, and main content match. Then delete `sessionStorage["htmx-history-cache"]` and press Back once. That is the miss: same document, correct contents, and a request that carries `HX-History-Restore-Request`.
- Log out with a full navigation, then press Back. Private content must not reappear from the snapshot. This fails in projects that never revalidate history.
- A widget that needs JavaScript still works after a swap and after a cache restore, not only on the first full load.
- Turn JavaScript off, or omit the htmx script, and submit the same form. It should `303` to the right document.

In the browser's network panel, htmx requests are XHR/fetch with `HX-Request`. Response headers of a `303` you issued will not appear there; you will see the followed response. `htmx.logAll()` prints every htmx event. The unminified build is the one to step through when a swap target is wrong.

## 10. Anti-patterns

**Returning a fragment for a boost that replaces `body`.** The fragment becomes the page. The header and the script tags that lived in the body are gone. Return a document, or stop targeting `body`.

**Returning a full document into a region target.** The region now contains a second page. Select the region, or return the fragment.

**`HX-Redirect` for an ordinary move.** It loads a new document and defeats the swap. Save it for a shell change.

**`location.href` in application code for the same reason.** If htmx can go there with `HX-Location` or a boosted link, a manual assignment is a reload you did not mean. An exception is leaving the origin, or starting a download that must be a navigation.

**Trusting an inherited target.** A parent `hx-target` or `hx-boost` will capture a new button someone added inside it. Set the target on the control, or opt out with `hx-boost="false"` / `hx-disinherit`, when the inherited behavior is not obviously true.

**Binding JavaScript on first load only.** The second visit is a swap. Use delegation or `htmx.onLoad`, and make it safe to run twice.

**A `<script>` in the `<head>` of a partial, expected to run.** It will not. Put the behavior in a script that is already on the page, or in the fragment body with an idempotent guard.

**Dropping `action` and `method`.** The form becomes a script-only control. Keep the native attributes.

**A script that reimplements swap, pushState, or "submit this form with fetch".** You now have two clients. htmx is the client.

**A second source of truth.** A JSON store, a custom element that renders itself, and the server HTML will drift. The server HTML is the state the user is looking at.

**Ignoring history and auth.** Shipping boost without a cache-miss test and a logout-then-back test leaves private HTML in `sessionStorage` and a miss path that returns the wrong shape. `hx-history="false"` on the private pages is the exception you choose on purpose, not an afterthought.

## 11. Checklist for coding agents

Use this before changing htmx behavior. Read the project's existing htmx notes and markup first; match them unless they contradict this guide.

1. Classify the interaction: document navigation, in-shell navigation, fragment update, poll, or download. Do not treat those as one feature.
2. Name the request headers a correct client will send (`HX-Request`, `HX-Boosted`, `HX-Target`, `HX-History-Restore-Request`) and the status and body for each.
3. Make `hx-target` and `hx-swap` explicit when inheritance could pull in the wrong region or scroll the window.
4. Keep `href`, `method`, and `action`. The no-JS path should still be a real request.
5. If the path changes, say how the URL changes (`hx-boost`, `hx-push-url`, `HX-Location`, `HX-Replace-Url`). If the shell changes, use a document load (`HX-Redirect` or a normal `303`), not a fragment under the old header.
6. If the response is a `303`, assume htmx will not see headers on that response.
7. Decide what Back shows. Confirm the history element, the miss response, and whether this screen is safe to snapshot.
8. If you added or swapped a widget, initialize it from a delegated listener or `htmx.onLoad`, and survive a second call and `htmx:historyRestore`.
9. Add HTTP tests for the response branches you touched. Add a browser pass for navigation, history, and any script you wired up. Include logout and Back if the screen shows private data.
10. Do not add a helper, a dependency, or a client-side lifecycle unless the markup cannot say the same thing.

## In this repository

Track Anything's choices — the content target, the three redirect responses, which links stay real navigations, and how page scripts survive a swap — are in [HTMX-IMPLEMENTATION.md](HTMX-IMPLEMENTATION.md). That file is not part of the portable guide.
