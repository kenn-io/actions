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
	positions := punctuation(source)
	for _, i := range positions {
		if source[i] == '[' || source[i] == ']' {
			source = source[:i] + " " + source[i+1:]
		}
	}
	for _, i := range punctuation(source) {
		if source[i] != '|' || i == 0 || i+1 == len(source) || strings.ContainsAny(source[i-1:i+2], " \t") || source[i-1] == '|' || source[i+1] == '|' {
			continue
		}
		start, end := i, i+1
		for start > 0 && !unicode.IsSpace(rune(source[start-1])) && !strings.ContainsRune(";&|", rune(source[start-1])) {
			start--
		}
		for end < len(source) && !unicode.IsSpace(rune(source[end])) && !strings.ContainsRune(";&|", rune(source[end])) {
			end++
		}
		return append(synopses(source[:i]+source[end:]), synopses(source[:start]+source[i+1:])...)
	}
	return []string{source}
}

func placeholder(word string) bool {
	return !strings.HasPrefix(word, "-") && (strings.HasPrefix(word, "<") || strings.HasPrefix(word, "{") || (strings.ToUpper(word) == word && strings.ContainsAny(word, "ABCDEFGHIJKLMNOPQRSTUVWXYZ")))
}
