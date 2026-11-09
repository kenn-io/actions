package main

import (
	"fmt"
	"github.com/spf13/cobra"
	"os"
	"path/filepath"
)

func main() {
	root := &cobra.Command{Use: filepath.Base(os.Args[0]), SilenceErrors: true, SilenceUsage: true}
	root.PersistentFlags().String("config", "", "Config path")
	group := &cobra.Command{Use: "group", Short: "Grouped commands", Run: func(cmd *cobra.Command, _ []string) { _ = cmd.Help() }}
	group.Flags().Bool("local", false, "Parent flag")
	child := &cobra.Command{Use: "child <id>", Aliases: []string{"alias"}, Short: "Read an ID", Run: func(*cobra.Command, []string) { panic("must only ask for help") }}
	child.Flags().IntP("limit", "n", 0, "Limit")
	group.AddCommand(child)
	named := &cobra.Command{Use: "named name", Run: func(cmd *cobra.Command, _ []string) { _ = cmd.Help() }}
	named.AddCommand(&cobra.Command{Use: "child", Run: func(*cobra.Command, []string) { panic("must only ask for help") }})
	broken := &cobra.Command{Use: "broken", Run: func(*cobra.Command, []string) { panic("must only ask for help") }}
	broken.SetHelpFunc(func(*cobra.Command, []string) { fmt.Fprintln(os.Stderr, "child help failed"); os.Exit(2) })
	named.AddCommand(broken)
	root.AddCommand(named)
	completion := &cobra.Command{Use: "completion", Run: func(cmd *cobra.Command, _ []string) { _ = cmd.Help() }}
	completion.AddCommand(&cobra.Command{Use: "bash", Run: func(*cobra.Command, []string) { panic("must only ask for help") }})
	root.AddCommand(completion)
	plain := &cobra.Command{Use: "plain", Run: func(*cobra.Command, []string) { panic("must only ask for help") }}
	plain.Flags().String("log.level", "info", "Log level")
	root.AddCommand(plain)
	root.AddCommand(group, &cobra.Command{Use: "run <path>", Short: "Read a path", Run: func(*cobra.Command, []string) { panic("must only ask for help") }}, &cobra.Command{Use: "unsafe <path>", DisableFlagParsing: true, Short: "Must never execute", Run: func(*cobra.Command, []string) { panic("disabled flag parsing command executed") }})
	if err := root.Execute(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(2)
	}
}
