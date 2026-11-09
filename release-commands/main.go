package main

import (
	"context"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"strings"

	"github.com/spf13/pflag"
	"go.kenn.io/kit/selfupdate"
)

func main() { os.Exit(run(context.Background(), os.Args[1:], os.Stdout, os.Stderr)) }

func run(ctx context.Context, args []string, out, errOut io.Writer) int {
	flags := pflag.NewFlagSet("release-commands", pflag.ContinueOnError)
	flags.SetOutput(errOut)
	archive := flags.String("archive", "", "Release tar.gz archive")
	release := flags.String("release", "release", "Release tag for findings")
	binaries := flags.StringArray("binary", nil, "Binary to check, repeatable")
	if err := flags.Parse(args); err != nil {
		return 2
	}
	if *archive == "" || len(*binaries) == 0 || flags.NArg() == 0 {
		fmt.Fprintln(errOut, "usage: release-commands --archive FILE --binary NAME [--release TAG] FILE...")
		return 2
	}
	dir, err := os.MkdirTemp("", "release-commands-*")
	if err != nil {
		fmt.Fprintln(errOut, err)
		return 1
	}
	defer os.RemoveAll(dir)
	if err := selfupdate.ExtractTarGz(*archive, dir); err != nil {
		fmt.Fprintln(errOut, err)
		return 1
	}
	paths := map[string]string{}
	err = filepath.WalkDir(dir, func(path string, entry fs.DirEntry, err error) error {
		if err == nil && entry.Type().IsRegular() && slices.Contains(*binaries, entry.Name()) && (paths[entry.Name()] == "" || strings.Count(path, "/") < strings.Count(paths[entry.Name()], "/")) {
			paths[entry.Name()] = path
		}
		return err
	})
	if err != nil {
		fmt.Fprintln(errOut, err)
		return 1
	}
	cache := helpCache{}
	failed := false
	for _, file := range flags.Args() {
		blocks, err := fileExamples(file, *binaries)
		if err != nil {
			fmt.Fprintln(errOut, err)
			failed = true
			continue
		}
		for _, block := range blocks {
			blockFailed, firstChecked := false, len(block.commands)
			for index, example := range block.commands {
				problem := ""
				invocations, _ := commands(example.text)
				for _, args := range invocations {
					if !slices.Contains(*binaries, args[0]) {
						continue
					}
					firstChecked = min(firstChecked, index)
					binary := paths[args[0]]
					if binary == "" {
						problem = fmt.Sprintf("%s: not in %s", args[0], filepath.Base(*archive))
						break
					}
					if err := cache.check(ctx, binary, args[1:]); err != nil {
						problem = fmt.Sprintf("%s %s", *release, err)
						break
					}
				}
				blockFailed = blockFailed || problem != ""
				if problem != "" && block.marker == "" {
					fmt.Fprintf(errOut, "%s:%d: %s: %s\n", file, example.line, strings.TrimSpace(example.text[:commentIndex(example.text)]), strings.Join(strings.Fields(problem), " "))
					failed = true
				}
			}
			if firstChecked < len(block.commands) && !blockFailed && block.marker != "" {
				first := block.commands[firstChecked]
				message := fmt.Sprintf("passes; remove %s: %s", block.marker, strings.TrimSpace(first.text[:commentIndex(first.text)]))
				fmt.Fprintf(out, "%s:%d: %s\n", file, first.line, message)
			}
		}
	}
	if failed {
		return 1
	}
	return 0
}

// fileExamples reads file's examples; a listed installer must yield at least one.
func fileExamples(file string, binaries []string) ([]exampleBlock, error) {
	data, err := os.ReadFile(file)
	if err != nil {
		return nil, err
	}
	blocks, err := examples(file, data, binaries)
	if err == nil && len(blocks) == 0 && !slices.Contains(markdownExtensions, filepath.Ext(file)) {
		return nil, fmt.Errorf("%s: no commands for %s", file, strings.Join(binaries, ", "))
	}
	return blocks, err
}
