package siblingpins

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestCheckReportsPinsOffMain(t *testing.T) {
	requests := map[string]int{}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests[r.URL.Path]++
		if r.Header.Get("Authorization") != "Bearer read-token" {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		switch r.URL.Path {
		case "/repos/kenn-io/forge/compare/main...aaaa":
			_, _ = w.Write([]byte(`{"status":"behind","ahead_by":0,"behind_by":3}`))
		case "/repos/kenn-io/kit/compare/main...v0.38.0":
			_, _ = w.Write([]byte(`{"status":"identical","ahead_by":0,"behind_by":0}`))
		case "/repos/kenn-io/forge/compare/main...bbbb":
			_, _ = w.Write([]byte(`{"status":"diverged","ahead_by":1,"behind_by":4}`))
		case "/repos/kenn-io/kit-ui/compare/main...cccc":
			_, _ = w.Write([]byte(`{"status":"ahead","ahead_by":2,"behind_by":0}`))
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	t.Cleanup(server.Close)

	findings, err := GitHub{Client: server.Client(), BaseURL: server.URL, Token: "read-token"}.Check(t.Context(), []Pin{
		{File: "go.mod", Repo: "forge", Ref: "aaaa", Source: "go.mod: on main"},
		{File: "go.mod", Repo: "kit", Ref: "v0.38.0", Source: "go.mod: tag on main"},
		{File: "go.mod", Repo: "forge", Ref: "bbbb", Source: "go.mod: squashed pull request head"},
		{File: "web/package.json", Repo: "kit-ui", Ref: "cccc", Source: "web/package.json: open pull request"},
		{File: "web/bun.lock", Repo: "kit-ui", Ref: "cccc", Source: "web/bun.lock: open pull request"},
		{File: "desktop/package.json", Repo: "app-ui", Ref: "dddd", Source: "desktop/package.json: missing"},
	})
	require.NoError(t, err)
	require.Equal(t, []Finding{
		{File: "go.mod", Repo: "forge", Ref: "bbbb", Source: "go.mod: squashed pull request head", Problem: "bbbb is not on kenn-io/forge main (unmerged commits: 1); pin a commit on main instead"},
		{File: "web/package.json", Repo: "kit-ui", Ref: "cccc", Source: "web/package.json: open pull request", Problem: "cccc is not on kenn-io/kit-ui main (unmerged commits: 2); pin a commit on main instead"},
		{File: "web/bun.lock", Repo: "kit-ui", Ref: "cccc", Source: "web/bun.lock: open pull request", Problem: "cccc is not on kenn-io/kit-ui main (unmerged commits: 2); pin a commit on main instead"},
		{File: "desktop/package.json", Repo: "app-ui", Ref: "dddd", Source: "desktop/package.json: missing", Problem: "kenn-io/app-ui has no revision dddd, or this check cannot read the repository"},
	}, findings)
	require.Equal(t, "go.mod: squashed pull request head: bbbb is not on kenn-io/forge main (unmerged commits: 1); pin a commit on main instead", findings[0].String())
	require.Equal(t, 1, requests["/repos/kenn-io/kit-ui/compare/main...cccc"], "pins naming one revision share a request")
}

func TestCheckRetriesServerErrors(t *testing.T) {
	calls := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		calls++
		if calls == 1 {
			w.Header().Set("Retry-After", "0")
			w.WriteHeader(http.StatusBadGateway)
			return
		}
		_, _ = w.Write([]byte(`{"status":"ahead","ahead_by":1}`))
	}))
	t.Cleanup(server.Close)

	findings, err := GitHub{Client: server.Client(), BaseURL: server.URL}.Check(t.Context(), []Pin{{Repo: "forge", Ref: "bbbb", Source: "go.mod"}})
	require.NoError(t, err)
	require.Len(t, findings, 1)
	require.Equal(t, 2, calls)
}

// A check that cannot ask GitHub must fail rather than pass with no findings.
func TestCheckFailsWhenGitHubRefuses(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
	}))
	t.Cleanup(server.Close)

	findings, err := GitHub{Client: server.Client(), BaseURL: server.URL}.Check(t.Context(), []Pin{{Repo: "forge", Ref: "aaaa", Source: "go.mod"}})
	require.ErrorContains(t, err, "401")
	require.Nil(t, findings)
}

// Bun records a tag dependency by the tag object's hash. The check finds the
// tag that points at it, and a hash no tag points at still fails.
func TestCheckResolvesTagObjectHashes(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/repos/kenn-io/kata/git/matching-refs/tags":
			_, _ = w.Write([]byte(`[
				{"ref":"refs/tags/v0.14.2","object":{"type":"commit","sha":"c6685725ffffffffffffffffffffffffffffffff"}},
				{"ref":"refs/tags/v0.14.3","object":{"type":"tag","sha":"c6685725ecd6a31aaea83cf98908e0c866c5d434"}}
			]`))
		case "/repos/kenn-io/kata/compare/main...v0.14.3":
			_, _ = w.Write([]byte(`{"status":"behind","ahead_by":0}`))
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	t.Cleanup(server.Close)

	findings, err := GitHub{Client: server.Client(), BaseURL: server.URL}.Check(t.Context(), []Pin{
		{File: "bun.lock", Repo: "kata", Ref: "c668572", Source: "bun.lock: tag object"},
		{File: "bun.lock", Repo: "kata", Ref: "0123456", Source: "bun.lock: unknown hash"},
	})
	require.NoError(t, err)
	require.Equal(t, []Finding{
		{File: "bun.lock", Repo: "kata", Ref: "0123456", Source: "bun.lock: unknown hash", Problem: "kenn-io/kata has no revision 0123456, or this check cannot read the repository"},
	}, findings)
}
