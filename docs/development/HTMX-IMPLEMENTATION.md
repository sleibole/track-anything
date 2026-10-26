# HTMX in this application

This file is how Track Anything uses HTMX. General practice that applies to other Go/HTMX projects is in [HTMX.md](HTMX.md).

HTMX 2.0.11 (`static/htmx.min.js`) swaps the children of `#content`. It does not replace the document shell.

The shell invariant: `document.body` and the header stay the same nodes across in-app navigation. `<main id="content">` stays the same element too. The swap is `innerHTML`, so only its children change. Log out, login, signup, change password, and an expired session load a new document, and then those nodes are new.

Do not set `hx-boost` on `<body>` or `<main>`. Login and signup live inside `<main>`, and a boosted header link with no target would swap `<body>` with a content fragment and remove the header. Boost sits on each in-app anchor, with `hx-target="#content"`.

## What the server returns

`render` writes the `layout` template, or the `partial` template when `HX-Request` is set. The partial is a `<title>` plus the page's `content` block. HTMX removes the title from the fragment and sets `document.title`. The block is what lands in `#content`. A partial has no `<!doctype html>` and no header. It ends with an inert `<template data-page-meta>` holding that page's canonical, description, social, and robots tags. `static/app.js` copies those into `<head>` after a `#content` swap and when history restores the fragment, so the title and the discovery tags match the page on screen. `X-Robots-Tag` is a response header and applies to both shapes. `render` sends `Vary: HX-Request, Cookie` and `Cache-Control: private, no-store`, so a cache does not reuse a fragment as the document or one viewer's page as another's.

`<main>` sets `hx-history-elt`, `hx-target="#content"`, and `hx-swap="innerHTML show:none"`. History saves and restores the inner HTML of `#content`, not the body. A cache miss GETs the URL with `HX-Request` and swaps that partial into `#content`. It must not become a document reload. The saved history item also stores the document title from the partial.

`show:none` on `<main>` is what log forms inherit, so a response that still swaps `#content` does not scroll. Page links override it with `show:window:top`. The 30-second refresh does not use that swap: it targets itself with `hx-swap="none"` and updates trackers out of band. A successful in-place log does the same with `HX-Reswap: none`.

The `htmx-config` meta tag swaps 2xx, 3xx, and 4xx responses. A 204 is not swapped. A 5xx is not swapped. A number **Log again** error still sends `HX-Retarget` for that tracker's log control. That is the one swap smaller than the page.

## Three redirect behaviors

These are three different responses. Do not fold them into one helper that always sends the same header.

1. **Normal HTTP request.** `303 See Other`. Every non-HTMX post, including the non-HTMX branch of `redirectTo` and `redirectBack`. The browser loads the new document.
2. **HTMX navigation within the application.** `HX-Location` from `redirectTo`. HTMX GETs the path and swaps it into `#content` with `innerHTML show:window:top`. History is pushed when the path differs from `HX-Current-URL`. The push is skipped when the path is already current, so renaming a household does not add a duplicate history entry. The shell and header stay. `redirectTo` does not send `HX-Redirect`.
3. **HTMX navigation that must reload the document.** `HX-Redirect`, which assigns `location.href`. `requireUser` sends this when `HX-Request` is set and there is no session, so the login page replaces the header. `POST /start` sends it when the submit creates a guest session, because the header changes from logged out to logged in. An existing session uses `redirectTo` instead. Use `HX-Redirect` when the shell has to change. Do not use it for an ordinary page change. A normal signed-out request stays a `303`.

A same-page `hx-post` may answer with a plain `303` (`redirectBack`, share on and off, settings time zone, send verification email). The browser follows that redirect inside the HTMX request, the final content replaces the children of `#content`, and the address bar is already correct, so nothing is pushed. That is the in-app case with no history update. It is not a fourth mechanism and it is not `HX-Redirect`. Leave `redirectBack` as a `303`.

## Links

`nav` in `icons.go` prints this, with `href` last:

```html
hx-boost="true" hx-target="#content" hx-swap="innerHTML show:window:top" href="..."
```

`hx-boost` on the anchor uses that `href`, pushes the response URL, and still lets Ctrl-click or Cmd-click open a tab. Header links need the explicit target because the header is outside `<main>`.

The helper is on the brand, Charts, Settings, tracker and household names, New tracker, Edit, back links, and message or join links whose destination is `/`.

These stay ordinary links or forms:

- Log in and Sign up, including the logged-out home and a message that links to `/login`
- Log out
- Change password
- Download CSV (`Content-Disposition: attachment`)

Chart and overlay controls keep their own `hx-get`, `hx-push-url`, and `show:none`. They use the same history element.

Without JavaScript the `href` or form `action` still navigates.

## Forms

In-app forms set `hx-post` and keep a real `action`. Create and save tracker, archive, and join call `redirectTo`, so an HTMX success is `HX-Location` and the address bar follows. Create household, rename, leave, and move already did.

Share on and off, the time zone, and verification email `303` back to the page already showing. The swapped page includes the new link or the "saved" line. The query string on a time-zone save is not pushed.

## Scripts

`htmx.min.js` and `app.js` are in the layout head. They survive a swap.

The install footer and dialog sit in the layout after `<main>`, so they are outside `#content` and are not stored in history. An inline script in the head listens for `beforeinstallprompt` while the first page loads. `app.js` keeps that event and binds the dialog once. A content swap does not add a second dialog or a second listener. If a swap would remove an open dialog, the scroll lock is cleared first.

`reorder.js` is in the dashboard content, not the head. A home page reached by a swap still loads it. The file ignores a second run, and its listeners stay on `document`, so a cached return to Home can still drag.

The Turnstile API script is in the layout for signup, login, and verified settings. `app.js` adds that same script when a swap contains `.cf-turnstile` and the script is not already on the page. `htmx:historyRestore` clears a restored widget and renders it again, including when Settings comes back from the history cache rather than from a new click. A cache miss returns fresh widget markup and the usual settle handler renders it.
