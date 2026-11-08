package main

import (
	"io"
	"slices"
	"strings"
	"unicode"

	"mvdan.cc/sh/v3/expand"
	"mvdan.cc/sh/v3/syntax"
)

var shellExpansion = &expand.Config{
	CmdSubst: func(w io.Writer, _ *syntax.CmdSubst) error { _, err := io.WriteString(w, "0"); return err },
	Env:      expand.FuncEnviron(func(name string) string { return "$" + name }),
}

// Synopsis expansion preserves shell quoting for the shell parser that follows it.
func commands(line string) ([][]string, error) {
	// Protect only unquoted placeholders from shell redirection parsing.
	positions := punctuation(line)
	for _, start := range slices.Backward(positions) {
		if line[start] != '<' {
			continue
		}
		end := strings.IndexByte(line[start:], '>')
		if end < 0 || strings.ContainsAny(line[start+1:start+end], " \t\n<>") {
			continue
		}
		value := line[start : start+end+1]
		replacement := "0"
		if value == "<command>" || value == "<subcommand>" {
			replacement = ""
		}
		line = line[:start] + replacement + line[start+end+1:]
	}
	var result [][]string
	for _, source := range synopses(line) {
		tree, err := syntax.NewParser().Parse(strings.NewReader(source), "")
		if err != nil {
			return nil, err
		}
		syntax.Walk(tree, func(node syntax.Node) bool {
			if _, substitution := node.(*syntax.CmdSubst); substitution {
				return false
			}
			call, ok := node.(*syntax.CallExpr)
			if !ok || err != nil {
				return err == nil
			}
			var args []string
			for _, word := range call.Args {
				var value string
				value, err = expand.Literal(shellExpansion, word)
				if err != nil {
					return false
				}
				if value != "..." && value != "…" {
					args = append(args, value)
				}
			}
			if len(args) > 0 {
				result = append(result, args)
			}
			return true
		})
		if err != nil {
			return nil, err
		}
	}
	return result, nil
}

// Only unquoted, unescaped punctuation participates in synopsis syntax.
func punctuation(source string) []int {
	var positions []int
	var quote byte
	for i := 0; i < len(source); i++ {
		c := source[i]
		if c == '\\' && quote != '\'' {
			i++
			continue
		}
		if quote != 0 {
			if c == quote {
				quote = 0
			}
			continue
		}
		if c == '\'' || c == '"' {
			quote = c
			continue
		}
		positions = append(positions, i)
	}
	return positions
}

func synopses(source string) []string {
	plain := []byte(source)
	for _, i := range punctuation(source) {
		if source[i] == '[' || source[i] == ']' {
			plain[i] = ' '
		}
		if source[i] != '|' || i == 0 || i+1 == len(source) || source[i-1] == '|' || source[i+1] == '|' {
			continue
		}
		// A spaced bar separates alternatives only inside optional brackets; elsewhere it is a pipeline.
		spaced := strings.ContainsAny(source[i-1:i+2], " \t")
		keep := func(c rune) bool {
			return !strings.ContainsRune("[]", c) && (spaced || !unicode.IsSpace(c) && !strings.ContainsRune(";&|", c))
		}
		start, end := len(strings.TrimRightFunc(source[:i], keep)), len(source)-len(strings.TrimLeftFunc(source[i+1:], keep))
		if !spaced || start > 0 && source[start-1] == '[' && end < len(source) && source[end] == ']' {
			return append(synopses(source[:i]+source[end:]), synopses(source[:start]+source[i+1:])...)
		}
	}
	return []string{string(plain)}
}

func placeholder(word string) bool {
	return !strings.HasPrefix(word, "-") && (strings.HasPrefix(word, "<") || strings.HasPrefix(word, "{") || (strings.ToUpper(word) == word && strings.ContainsAny(word, "ABCDEFGHIJKLMNOPQRSTUVWXYZ")))
}
