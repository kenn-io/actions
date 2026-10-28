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
	"go.kenn.io/actions/sibling-pins/siblingpins"
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

	pins, err := siblingpins.Collect(os.DirFS(*root))
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}
	// GitHub Actions sets GITHUB_API_URL; GITHUB_TOKEN or GH_TOKEN reads
	// private siblings.
	github := siblingpins.GitHub{
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
		messages := make([]string, len(findings))
		for i, finding := range findings {
			messages[i] = finding.String()
		}
		fmt.Fprintf(os.Stderr, "Sibling pins must be on %s before this merges; see https://github.com/kenn-io/actions/tree/main/sibling-pins#fix-a-finding:\n  %s\n",
			siblingpins.Branch, strings.Join(messages, "\n  "))
		return 1
	}
	fmt.Fprintf(os.Stderr, "All %d sibling pins are on %s\n", len(pins), siblingpins.Branch)
	return 0
}
