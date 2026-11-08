package main

import (
	"fmt"
	"github.com/spf13/cobra"
	"os"
	"path/filepath"
)

func main() {
	root := &cobra.Command{Use: filepath.Base(os.Args[0]), SilenceErrors: true, SilenceUsage: true}
	if root.Use == "prose" {
		root.Long = "--meta key... optional metadata\n--ghost  prose"
	}
	root.PersistentFlags().String("config", "", "Config path")
	if root.Use == "compact" {
		root.PersistentFlags().Bool("json", false, "JSON output")
		root.PersistentFlags().String("project", "", "Project")
		root.SetUsageTemplate("Usage:\n  {{.UseLine}}\n{{if .HasAvailableSubCommands}}  {{.CommandPath}} [command]\n{{end}}\nFlags:\n{{.LocalFlags.FlagUsages}}\n{{if .HasAvailableInheritedFlags}}Global: --json, --project <string>. Details: compact --help\n{{end}}")
	}
	group := &cobra.Command{Use: "group", Short: "Grouped commands", Run: func(cmd *cobra.Command, _ []string) { _ = cmd.Help() }}
	group.Flags().Bool("local", false, "Parent flag")
	child := &cobra.Command{Use: "child <id>", Aliases: []string{"alias"}, Short: "Read an ID", Run: func(*cobra.Command, []string) { panic("must only ask for help") }}
	child.Flags().IntP("limit", "n", 0, "Limit\nAdditional limit details")
	group.AddCommand(child)
	root.AddCommand(group, &cobra.Command{Use: "run <path>", Short: "Read a path", Run: func(*cobra.Command, []string) { panic("must only ask for help") }}, &cobra.Command{Use: "unsafe <path>", DisableFlagParsing: true, Short: "Must never execute", Run: func(*cobra.Command, []string) { panic("disabled flag parsing command executed") }})
	if err := root.Execute(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(2)
	}
}
