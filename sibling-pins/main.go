// sibling-pins checks that every revision a repository pins from a sibling
// kenn-io repository is on that repository's main branch. A pull request may
// pin a sibling's pull request build while it is open, but main may not: a
// squash merge never puts that commit on the sibling's main, and Go cannot
// download a commit reachable only through a pull request ref.
package main

import (
	"cmp"
	"context"
	"fmt"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"time"

	"github.com/spf13/pflag"
)

func main() { os.Exit(run()) }

func run() int {
	flags := pflag.NewFlagSet("sibling-pins", pflag.ContinueOnError)
	root := flags.String("root", ".", "Repository checkout whose dependency files are checked")
	if err := flags.Parse(os.Args[1:]); err != nil {
		return 2
	}
	if flags.NArg() != 0 {
		fmt.Fprintln(os.Stderr, "usage: sibling-pins [--root DIRECTORY]")
		return 2
	}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
	defer stop()

	pins, err := Collect(os.DirFS(*root))
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}
	// GitHub Actions sets GITHUB_API_URL; GITHUB_TOKEN or GH_TOKEN reads
	// private siblings.
	github := GitHub{
		Client:  &http.Client{Timeout: 30 * time.Second},
		BaseURL: cmp.Or(os.Getenv("GITHUB_API_URL"), "https://api.github.com"),
		Token:   cmp.Or(os.Getenv("GITHUB_TOKEN"), os.Getenv("GH_TOKEN")),
	}
	findings, err := github.Check(ctx, pins)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}
	if len(findings) > 0 {
		fmt.Fprintf(os.Stderr, "Sibling pins must be on %s before this merges; see https://github.com/kenn-io/actions/tree/main/sibling-pins#fix-a-finding:\n  %s\n",
			Branch, strings.Join(findings, "\n  "))
		return 1
	}
	fmt.Fprintf(os.Stderr, "All %d sibling pins are on %s\n", len(pins), Branch)
	return 0
}
