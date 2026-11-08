package main

import (
	"fmt"
	"github.com/spf13/cobra"
	"os"
)

func main() {
	root := &cobra.Command{Use: "stub", SilenceErrors: true, SilenceUsage: true}
	root.PersistentFlags().String("config", "", "Config path")
	root.Flags().Bool("version", false, "Version")
	root.PersistentFlags().Bool("hidden", false, "Hidden flag")
	_ = root.PersistentFlags().MarkHidden("hidden")
	group := &cobra.Command{Use: "group", Short: "Grouped commands", Run: func(cmd *cobra.Command, _ []string) { _ = cmd.Help() }}
	group.Flags().Bool("local", false, "Parent flag")
	child := &cobra.Command{Use: "child <id>", Aliases: []string{"alias"}, Short: "Read an ID", Run: func(*cobra.Command, []string) { panic("must only ask for help") }}
	child.Flags().IntP("limit", "n", 0, "Limit")
	group.AddCommand(child)
	root.AddCommand(group, &cobra.Command{Use: "run <path>", Short: "Read a path", Run: func(*cobra.Command, []string) { panic("must only ask for help") }}, &cobra.Command{Use: "unsafe <path>", DisableFlagParsing: true, Short: "Must never execute", Run: func(*cobra.Command, []string) { panic("disabled flag parsing command executed") }})
	if err := root.Execute(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(2)
	}
}
