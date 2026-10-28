# First paint

This action fails when a page can paint before its stylesheets or fonts are in
place. Such a page flashes unstyled, or shows text in a fallback face and then
swaps it, on a slow connection.

## What it checks

The action reads each page's HTML and the CSS it links. It needs no browser
and checks a site of 60 pages in under a second. A page passes when it does
three things:

1. **Inlines [`hold-first-paint.js`](hold-first-paint.js) in `<head>`, before
   any stylesheet.** Chromium decodes even an embedded font only after its first
   paint, so it paints one frame in the fallback face. The script sets the
   page's opacity to 0 until its stylesheets and fonts load, for at most 3
   seconds. Pages without JavaScript are never hidden. The inline copy must
   match this action's copy line for line; comment lines above the code may
   differ. A page with a `script-src` Content Security Policy needs the
   script's hash in that policy.
2. **Links each stylesheet with a plain `<link rel="stylesheet">` in `<head>`.**
   A stylesheet in `<body>`, or one a script swaps in after it loads, such as
   the `media="print"` `onload` trick, does not block first paint.
3. **Embeds every font in its stylesheets as a `data:` URL.** A font in a
   separate file is discovered only after the stylesheet is parsed, so the
   browser paints before the font arrives. The check follows `@import` and reads
   inline `<style>` too. A stylesheet on another origin, such as a font
   service's, fails, since its fonts are separate files.

A page with no stylesheets passes. Print-only stylesheets are skipped.

The check cannot see a stylesheet or font that a script adds at runtime, such
as one a route loads on demand.

## Use it

Build the site, then check the built HTML. Every `.html` file under `dist` is
checked.

```yaml
jobs:
  first-paint:
    name: First paint
    runs-on: ubuntu-latest
    timeout-minutes: 10
    steps:
      - uses: actions/checkout@3d3c42e5aac5ba805825da76410c181273ba90b1 # v7.0.1
        with:
          persist-credentials: false
      # Build the site here.
      - uses: kenn-io/actions/first-paint@<commit on main>
        with:
          dist: dist
```

For pages a server renders on request, such as a UI a Go binary embeds, start
the server in an earlier step and list its pages:

```yaml
      - name: Start the server
        run: ./bin/server --listen 127.0.0.1:8080 &
      - uses: kenn-io/actions/first-paint@<commit on main>
        with:
          urls: |
            http://127.0.0.1:8080/
            http://127.0.0.1:8080/settings
```

`dist` and `urls` can be combined. Pin the action to a full commit on this
repository's `main`. When you move the pin, copy that commit's
`hold-first-paint.js` too, or the check fails.

## Embed fonts

With Vite, Astro, or SvelteKit, inline font files as data URLs:

```js
build: {
  assetsInlineLimit: (file) => (file.endsWith(".woff2") ? true : undefined),
},
```

Declare a face only in the stylesheet of the pages that use it, since every
page that loads the stylesheet downloads the embedded font.

## Run it locally

From a checkout of this repository:

```sh
cd first-paint
npm ci
node check-first-paint.mjs --dist <built site>
node check-first-paint.mjs --url http://127.0.0.1:8080/
```

The check exits 1 and lists each problem by page.
