package siblingpins

import (
	"context"
	"encoding/json/v2"
	"fmt"
	"io"
	"net/http"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/cenkalti/backoff/v7"
)

// Branch is the sibling branch every pin must be reachable from.
const Branch = "main"

// Bun records a tag dependency by the tag object's abbreviated hash, which the
// compare API does not resolve.
var objectHash = regexp.MustCompile(`^[0-9a-f]{7,40}$`)

// GitHub reads the compare and tag APIs. BaseURL is https://api.github.com in
// production. Token needs contents read access to private siblings.
type GitHub struct {
	Client  *http.Client
	BaseURL string
	Token   string
}

// Finding describes a pin that is not on its sibling's main branch.
type Finding struct {
	Pin
	Revision string // Revision actually compared; Ref retains the pin's file evidence.
	Problem  string
}

func (f Finding) String() string {
	return f.Source + ": " + f.Problem
}

// Check reports each pin whose revision is not reachable from its
// repository's main branch. Pins naming the same revision share one request.
// The error reports failures to ask GitHub, not off-main pins.
func (g GitHub) Check(ctx context.Context, pins []Pin) ([]Finding, error) {
	type key struct{ repo, ref string }
	verdicts := make(map[key]Finding)
	var findings []Finding
	for _, pin := range pins {
		k := key{pin.Repo, pin.Ref}
		finding, seen := verdicts[k]
		if !seen {
			var err error
			finding.Revision, finding.Problem, err = g.offMain(ctx, pin.Repo, pin.Ref)
			if err != nil {
				return nil, fmt.Errorf("%s: %w", pin.Source, err)
			}
			verdicts[k] = finding
		}
		if finding.Problem != "" {
			findings = append(findings, Finding{Pin: pin, Revision: finding.Revision, Problem: finding.Problem})
		}
	}
	return findings, nil
}

// comparison is the part of a GitHub compare response the check reads.
type comparison struct {
	Status  string `json:"status"`
	AheadBy int    `json:"ahead_by"`
}

// offMain returns the compared revision and why it is off main, or an empty problem.
func (g GitHub) offMain(ctx context.Context, repo, ref string) (string, string, error) {
	var result comparison
	// per_page=1 keeps the commit list short; only the counts matter.
	found, err := g.get(ctx, fmt.Sprintf("/repos/%s/%s/compare/%s...%s?per_page=1", Owner, repo, Branch, ref), &result)
	if err != nil {
		return "", "", err
	}
	compared := ref
	if !found && objectHash.MatchString(ref) {
		tag, err := g.tagForObject(ctx, repo, ref)
		if err != nil {
			return "", "", err
		}
		if tag != "" {
			compared = tag
			found, err = g.get(ctx, fmt.Sprintf("/repos/%s/%s/compare/%s...%s?per_page=1", Owner, repo, Branch, tag), &result)
			if err != nil {
				return "", "", err
			}
		}
	}
	if !found {
		// GitHub also answers 404 for a private repository the token cannot
		// read, so the message names both causes.
		return compared, fmt.Sprintf("%s/%s has no revision %s, or this check cannot read the repository", Owner, repo, ref), nil
	}
	switch result.Status {
	case "identical", "behind":
		return compared, "", nil
	case "ahead", "diverged":
		return compared, fmt.Sprintf("%s is not on %s/%s %s (unmerged commits: %d); pin a commit on %s instead",
			compared, Owner, repo, Branch, result.AheadBy, Branch), nil
	default:
		return "", "", fmt.Errorf("GitHub compare %s/%s: unknown status %q", Owner, repo, result.Status)
	}
}

// tagForObject returns the tag whose ref points at an annotated tag object
// with the abbreviated hash, or "" when none does.
func (g GitHub) tagForObject(ctx context.Context, repo, hash string) (string, error) {
	for page := 1; ; page++ {
		var refs []struct {
			Ref    string `json:"ref"`
			Object struct {
				Type string `json:"type"`
				SHA  string `json:"sha"`
			} `json:"object"`
		}
		found, err := g.get(ctx, fmt.Sprintf("/repos/%s/%s/git/matching-refs/tags?per_page=100&page=%d", Owner, repo, page), &refs)
		if err != nil || !found || len(refs) == 0 {
			return "", err
		}
		for _, ref := range refs {
			if ref.Object.Type == "tag" && strings.HasPrefix(ref.Object.SHA, hash) {
				return strings.TrimPrefix(ref.Ref, "refs/tags/"), nil
			}
		}
		if len(refs) < 100 {
			return "", nil
		}
	}
}

// get decodes one GitHub API response into out. It returns found false for
// 404, retries rate limits and server errors, and fails on other statuses.
func (g GitHub) get(ctx context.Context, path string, out any) (bool, error) {
	found, err := backoff.Retry(ctx, func() (bool, error) { return g.getOnce(ctx, path, out) },
		backoff.WithMaxTries(4),
		backoff.WithMaxElapsedTime(2*time.Minute))
	if retryErr := backoff.AsRetryError(err); retryErr != nil {
		return false, retryErr.LastErr
	}
	return found, err
}

func (g GitHub) getOnce(ctx context.Context, path string, out any) (bool, error) {
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, g.BaseURL+path, nil)
	if err != nil {
		return false, backoff.Permanent(err)
	}
	request.Header.Set("Accept", "application/vnd.github+json")
	request.Header.Set("X-GitHub-Api-Version", "2022-11-28")
	if g.Token != "" {
		request.Header.Set("Authorization", "Bearer "+g.Token)
	}
	response, err := g.Client.Do(request)
	if err != nil {
		return false, err
	}
	defer response.Body.Close()
	failure := fmt.Errorf("GitHub %s: %s", path, response.Status)
	switch {
	case response.StatusCode == http.StatusOK:
	case response.StatusCode == http.StatusNotFound:
		return false, nil
	case response.StatusCode == http.StatusTooManyRequests || response.StatusCode >= 500:
		if seconds, parseErr := strconv.Atoi(response.Header.Get("Retry-After")); parseErr == nil {
			return false, backoff.RetryAfter(time.Duration(seconds)*time.Second, failure)
		}
		return false, failure
	default:
		return false, backoff.Permanent(failure)
	}
	// A pin off main also returns its changed files; the cap bounds a
	// pathological one.
	if err := json.UnmarshalRead(io.LimitReader(response.Body, 64<<20), out); err != nil {
		return false, backoff.Permanent(fmt.Errorf("decode GitHub %s: %w", path, err))
	}
	return true, nil
}
