package main

import (
	"testing"
	"testing/fstest"

	"github.com/stretchr/testify/require"
)

func TestCollectFindsEverySiblingPinForm(t *testing.T) {
	files := fstest.MapFS{
		"go.mod": {Data: []byte(`module go.kenn.io/app-client

go 1.27

require (
	github.com/spf13/pflag v1.0.10
	go.kenn.io/forge v0.4.1-0.20261020014253-5fc3781f38fc
	go.kenn.io/kit v0.38.0
	go.kenn.io/tools/sub/v2 v2.1.0
	go.kenn.io/docbank v0.15.3-0.20261014194428-07f6238bc79c // indirect
	go.kenn.io/roborev v0.68.0
	go.kenn.io/app v0.1.3-0.20260901183326-b3485973ae0c
)

replace go.kenn.io/roborev => go.kenn.io/roborev v0.68.1-0.20260922180251-64e5f2afa0d1

replace go.kenn.io/app => github.com/kenn-io/app-server v0.2.15-0.20261009174620-bbef853d70fd
`)},
		"examples/app/go.mod": {Data: []byte(`module example

require go.kenn.io/app-client v0.0.0

replace go.kenn.io/app-client => ../..
`)},
		"web/package.json": {Data: []byte(`{"dependencies": {
  "@kenn-io/app-ui": "git+https://github.com/kenn-io/app-ui.git/#b5b99f230e62f6346ec83ae74f17bcc55cc2dfa6",
  "@kenn-io/kit-ui": "github:kenn-io/kit-ui#bccce525981be5a2afbcb95fba29952052a659e1",
  "svelte": "^5.29.0"
}}`)},
		"web/bun.lock": {Data: []byte(`{
  "workspaces": {"": {"dependencies": {
    "@kenn-io/kit-ui": "github:kenn-io/kit-ui#bccce525981be5a2afbcb95fba29952052a659e1",
  }}},
  "packages": {
    "@kenn-io/kit-ui": ["@kenn-io/kit-ui@github:kenn-io/kit-ui#bccce52", {}, "kenn-io-kit-ui-bccce52"],
    "@kenn-io/brand": ["@kenn-io/brand@git+ssh://git@github.com/kenn-io/brand.git#79dc99be", {}],
  }
}`)},
		"Makefile":        {Data: []byte("huma-check:\n\tgo run go.kenn.io/kit/cmd/huma-check@3e1f59e9011e878ec595aa04aebc8a77c5292c4d -fix=false ./...\n")},
		"tools/Makefile":  {Data: []byte("HUMA_CHECK_VERSION := v0.25.1-0.20260915220235-532a5501741e\n\nlint:\n\tgo run go.kenn.io/kit/cmd/huma-check@$(HUMA_CHECK_VERSION) ./...\n")},
		".custom-gcl.yml": {Data: []byte("version: v2.14.0\nplugins:\n  - module: \"go.kenn.io/kit\"\n    import: \"go.kenn.io/kit/lint/gclplugin\"\n    version: \"v0.35.1\"\n")},
		// A plugin built from this checkout pins nothing.
		"kit/.custom-gcl.yml":      {Data: []byte("version: v2.14.0\nplugins:\n  - module: \"go.kenn.io/kit\"\n    import: \"go.kenn.io/kit/lint/gclplugin\"\n    path: \".\"\n")},
		".github/workflows/ci.yml": {Data: []byte("jobs:\n  pins:\n    steps:\n      - uses: actions/checkout@v7\n      - uses: kenn-io/actions/sibling-pins@fc37c96bb2d4\n      - uses: \"kenn-io/forge/.github/workflows/build.yml@v0.4.0\" # reusable workflow\n")},
		"tools/lint/action.yml":    {Data: []byte("runs:\n  using: composite\n  steps:\n    - uses: kenn-io/kit/actions/lint@v0.38.0\n")},
		// Installed packages are not this repository's pins.
		"web/node_modules/x/package.json": {Data: []byte(`{"dependencies": {"y": "github:kenn-io/other#deadbeef"}}`)},
	}

	pins, err := Collect(files)
	require.NoError(t, err)
	var got [][2]string
	for _, pin := range pins {
		got = append(got, [2]string{pin.Repo, pin.Ref})
	}
	require.ElementsMatch(t, [][2]string{
		{"kit", "v0.35.1"},
		{"kit", "3e1f59e9011e878ec595aa04aebc8a77c5292c4d"},
		{"kit", "532a5501741e"},
		{"app-server", "bbef853d70fd"},
		{"forge", "5fc3781f38fc"},
		{"kit", "v0.38.0"},
		{"tools", "sub/v2.1.0"},
		{"docbank", "07f6238bc79c"},
		{"roborev", "64e5f2afa0d1"},
		{"app-ui", "b5b99f230e62f6346ec83ae74f17bcc55cc2dfa6"},
		{"kit-ui", "bccce525981be5a2afbcb95fba29952052a659e1"},
		{"kit-ui", "bccce525981be5a2afbcb95fba29952052a659e1"},
		{"kit-ui", "bccce52"},
		{"brand", "79dc99be"},
		{"actions", "fc37c96bb2d4"},
		{"forge", "v0.4.0"},
		{"kit", "v0.38.0"},
	}, got)
}

// A replacement for one version does not cover a requirement that moved on.
func TestCollectChecksRequirementsAStaleReplacementMisses(t *testing.T) {
	pins, err := Collect(fstest.MapFS{"go.mod": {Data: []byte(`module m

require go.kenn.io/forge v0.4.1-0.20261019233853-b83608df900b

replace go.kenn.io/forge v0.4.0 => go.kenn.io/forge v0.4.1-0.20261020014253-5fc3781f38fc
`)}})
	require.NoError(t, err)
	var refs []string
	for _, pin := range pins {
		refs = append(refs, pin.Ref)
	}
	require.ElementsMatch(t, []string{"5fc3781f38fc", "b83608df900b"}, refs)
}

func TestCollectRejectsPinsItCannotCheck(t *testing.T) {
	for name, files := range map[string]fstest.MapFS{
		"fork replacement": {"go.mod": {Data: []byte(`module m

require go.kenn.io/forge v0.4.0

replace go.kenn.io/forge => github.com/someone/forge v0.4.1-0.20261020014253-5fc3781f38fc
`)}},
		"ref that would rewrite the comparison": {"package.json": {Data: []byte(`{"dependencies": {"a": "github:kenn-io/kit-ui#main...evil"}}`)}},
		"Makefile variable assigned elsewhere":  {"Makefile": {Data: []byte("lint:\n\tgo run go.kenn.io/kit/cmd/huma-check@$(HUMA_CHECK_VERSION) ./...\n")}},
	} {
		t.Run(name, func(t *testing.T) {
			_, err := Collect(files)
			require.Error(t, err)
		})
	}
}
