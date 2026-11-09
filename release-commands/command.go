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
	CmdSubst:  func(w io.Writer, _ *syntax.CmdSubst) error { _, err := io.WriteString(w, "$"); return err },
	ProcSubst: func(*syntax.ProcSubst) (string, error) { return "$", nil },
	Env: expand.FuncEnviron(func(name string) string {
		if name == "IFS" {
			return ""
		}
		return "$" + name
	}),
}

// Synopsis expansion preserves shell quoting for the shell parser that follows it.
func commands(line string) ([][]string, error) {
	for start := commentIndex(line); start < len(line); start = commentIndex(line) {
		end := strings.IndexByte(line[start:], '\n')
		if end < 0 {
			line = line[:start]
		} else {
			line = line[:start] + line[start+end:]
		}
	}
	// Protect only unquoted placeholders from shell redirection parsing.
	positions := punctuation(line)
	for _, start := range slices.Backward(positions) {
		if line[start] != '<' && line[start] != '[' {
			continue
		}
		closing := byte(']')
		if line[start] == '<' {
			closing = '>'
		}
		end := strings.IndexByte(line[start:], closing)
		if end < 0 || strings.ContainsAny(line[start+1:start+end], " \t\n<>") {
			continue
		}
		value := line[start : start+end+1]
		replacement, err := syntax.Quote(value, syntax.LangBash)
		if err != nil {
			return nil, err
		}
		if value == "<command>" || value == "<subcommand>" || value == "[flags]" || value == "[command]" {
			replacement = ""
		} else if line[start] == '[' {
			continue
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
			call, ok := node.(*syntax.CallExpr)
			if !ok || err != nil {
				return err == nil
			}
			var args []string
			for _, word := range call.Args {
				var values []string
				values, err = expand.Fields(shellExpansion, word)
				if err != nil {
					return false
				}
				if word.Lit() != "..." && word.Lit() != "…" {
					args = append(args, values...)
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
	quotes := "" // closing delimiters, innermost last; ")" ends a parenthesis opened inside a command substitution
	for i := 0; i < len(source); i++ {
		c, quote := source[i:i+1], quotes[max(len(quotes)-1, 0):]
		switch {
		case c == `\` && quote != "'":
			i++
		case quote != "'" && strings.HasPrefix(source[i:], "$(") || quote == ")" && c == "(":
			quotes, i = quotes+")", i+strings.IndexByte(source[i:], '(')
		case c == quote:
			quotes = quotes[:len(quotes)-1]
		case (c == "'" || c == `"`) && quote != "'" && quote != `"`:
			quotes += c
		case quote == "":
			positions = append(positions, i)
		}
	}
	return positions
}

func commentIndex(source string) int {
	tree, _ := syntax.NewParser(syntax.KeepComments(true)).Parse(strings.NewReader(source), "")
	first := len(source)
	syntax.Walk(tree, func(node syntax.Node) bool {
		if comment, ok := node.(*syntax.Comment); ok {
			first = min(first, int(comment.Pos().Offset()))
		}
		return true
	})
	return first
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
		start, end := 0, len(source)
		for _, boundary := range punctuation(source) {
			c := rune(source[boundary])
			if !strings.ContainsRune("[]", c) && (spaced || !unicode.IsSpace(c) && !strings.ContainsRune(";&|", c)) {
				continue
			}
			if boundary < i {
				start = boundary + 1
			} else if boundary > i {
				end = boundary
				break
			}
		}
		if !spaced || start > 0 && source[start-1] == '[' && end < len(source) && source[end] == ']' {
			return append(synopses(source[:i]+source[end:]), synopses(source[:start]+source[i+1:])...)
		}
	}
	return []string{string(plain)}
}

func placeholder(word string) bool {
	return !strings.HasPrefix(word, "-") && (strings.HasPrefix(word, "$") || strings.HasPrefix(word, "<") || strings.HasPrefix(word, "{") || (strings.ToUpper(word) == word && strings.ContainsAny(word, "ABCDEFGHIJKLMNOPQRSTUVWXYZ")))
}
