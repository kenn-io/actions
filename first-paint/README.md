# First paint

This action fails when a page can paint before its stylesheets or fonts are in
place. Such a page flashes unstyled, or shows text in a fallback face and then
swaps it, on a slow connection.

## What it checks

The action loads each page in Firefox and in Chromium. It holds back every
stylesheet and font response for 600 ms, so any page that paints before a late
request arrives paints first and fails. For each page it reports:

- a stylesheet or font that returned an error or failed to load;
- a stylesheet, or a stylesheet it imports, that the page requested before its
  first contentful paint and that finished after it;
- a font that failed to decode;
- visible text whose font had not loaded in the frame that first painted, so
  the text painted in a fallback face;
- fonts still loading 5 seconds after first paint.

The check follows redirects. It ignores fonts no visible text used at first
paint, stylesheets for other media such as print, and stylesheets the
page requests after its first paint, such as one a route loads on demand.

Firefox records no first contentful paint for some pages that the hold script
below kept hidden. For those pages the check uses the time the script revealed
the page.

## Use it

Build the site, then check the built HTML. Every `.html` file under `dist` is
served from a local server and checked.

```yaml
jobs:
  first-paint:
    name: First paint
    runs-on: ubuntu-latest
    timeout-minutes: 15
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

`dist` and `urls` can be combined. Set `browsers: firefox` to check one
browser. Pin the action to a full commit on this repository's `main`.

## Fix a finding

A page passes in both browsers when it does two things.

1. **Embed its fonts in its render-blocking stylesheet.** A font in a separate
   file is discovered only after the stylesheet is parsed, so the browser paints
   before the font arrives. With Vite, Astro, or SvelteKit, inline font files as
   data URLs:

   ```js
   build: {
     assetsInlineLimit: (file) => (file.endsWith(".woff2") ? true : undefined),
   },
   ```

   Import a font with `?url&no-inline` to keep that one as a separate file.
   Declare a face only in the stylesheet of the pages that use it, since every
   page that loads the stylesheet downloads the embedded font.

2. **Inline [`hold-first-paint.js`](hold-first-paint.js) in `<head>`, before
   any stylesheet.** Firefox decodes an embedded font before it first paints.
   Chromium paints one frame first, in the fallback face. The script sets the
   page's opacity to 0 until its stylesheets and fonts load, for at most 3
   seconds, and pages without JavaScript are never hidden. A page with a
   `script-src` Content Security Policy needs the script's hash in that policy.

A stylesheet that finishes after first paint did not block it. Load each
stylesheet the first paint needs with a plain `<link rel="stylesheet">` in
`<head>`, without a `media` swap or a script that adds it.

## Run it locally

From a checkout of this repository:

```sh
cd first-paint
npm ci
npx playwright install firefox chromium
node check-first-paint.mjs --dist <built site>
node check-first-paint.mjs --url http://127.0.0.1:8080/ --browser firefox
```

The check exits 1 and lists each problem by browser and page.
