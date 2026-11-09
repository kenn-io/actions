package main

import (
	"bytes"
	"cmp"
	"fmt"
	"path/filepath"
	"regexp"
	"slices"
	"strings"

	"github.com/yuin/goldmark/v2/ast"
	"github.com/yuin/goldmark/v2/extension"
	"github.com/yuin/goldmark/v2/parser"
	"mvdan.cc/sh/v3/syntax"
)

type example struct {
	text string
	line int
}
type exampleBlock struct {
	commands []example
	marker   string
}

const note = "Not in the latest release yet"

var (
	markdownExtensions  = []string{".md", ".mdx", ".markdown"}
	powershellCalls     = regexp.MustCompile(`(?im)(?:^|[;{}|])[ \t]*([\w-]+)[ \t]+`)
	powershellFunctions = regexp.MustCompile(`(?i)\bfunction\s+([\w-]+)`)
	powershellHelpers   = regexp.MustCompile(`(?im)^[ \t]*function\s+([\w-]+)\((?:\[string\])?(\$\w+)\)[ \t]*\{[ \t]*(write-[\w-]+)[ \t]+(\$\w+)(?:[ \t][^{}\r\n]*)?\}`)
	powershellArguments = regexp.MustCompile(`^[ \t]*('(?:''|[^'\r\n])*'|"[^"\r\n]*")`)
)

// installerStrings reads static, single-line output and one-step message helpers.
func installerStrings(source []byte, powershell bool) ([]example, error) {
	if !powershell {
		return shellStrings(source)
	}
	functions := map[string]bool{}
	for _, match := range powershellFunctions.FindAllSubmatch(code(source), -1) {
		functions[strings.ToLower(string(match[1]))] = false
	}
	for _, match := range powershellHelpers.FindAllSubmatch(code(source), -1) {
		_, helper := functions[strings.ToLower(string(match[3]))]
		if !helper && bytes.EqualFold(match[2], match[4]) {
			functions[strings.ToLower(string(match[1]))] = true
		}
	}
	var messages []example
	for _, match := range powershellCalls.FindAllSubmatchIndex(code(source), -1) {
		name := strings.ToLower(string(source[match[2]:match[3]]))
		forwarded, declared := functions[name]
		if !forwarded && (declared || !strings.HasPrefix(name, "write-")) {
			continue
		}
		rest := string(source[match[3]:])
		arg := powershellArguments.FindStringSubmatch(rest)
		if arg == nil {
			continue
		}
		text := arg[1]
		value := text[1 : len(text)-1]
		if text[0] == '\'' || !strings.ContainsAny(value, "$`") {
			messages = append(messages, example{value, bytes.Count(source[:match[3]], []byte{'\n'}) + 1})
		}
	}
	return messages, nil
}

// code blanks PowerShell comments and quoted text so only real calls remain matchable.
func code(source []byte) []byte {
	masked := bytes.Clone(source)
	var quote byte
	for i := 0; i < len(masked); i++ {
		switch c := masked[i]; {
		case quote == '<' && c == '#' && i+1 < len(masked) && masked[i+1] == '>':
			masked[i], masked[i+1], quote = ' ', ' ', 0
			i++
		case quote == '#' && c == '\n':
			quote = 0
		case quote == '"' && c == '`' && i+1 < len(masked):
			masked[i], masked[i+1] = ' ', ' '
			i++
		case quote != 0 && c == quote && quote != '<' && quote != '#':
			quote = 0
		case quote == 0 && c == '<' && i+1 < len(masked) && masked[i+1] == '#':
			masked[i], masked[i+1], quote = ' ', ' ', '<'
			i++
		case quote == 0 && c == '#' && (i == 0 || strings.IndexByte(" \t\r\n;{}|(", masked[i-1]) >= 0):
			masked[i], quote = ' ', '#'
		case quote == 0 && (c == '\'' || c == '"'):
			quote = c
		case quote != 0 && c != '\n':
			masked[i] = ' '
		}
	}
	return masked
}

// shellStrings reads output arguments from parsed calls, excluding redirects and heredocs.
func shellStrings(source []byte) ([]example, error) {
	tree, err := syntax.NewParser().Parse(bytes.NewReader(source), "")
	if err != nil {
		return nil, err
	}
	// Helpers forward their first argument directly to a printer.
	printers := map[string]bool{"echo": true, "printf": true}
	prints := func(call *syntax.CallExpr) bool {
		return len(call.Args) > 1 && (call.Args[0].Lit() == "echo" || call.Args[0].Lit() == "printf" && call.Args[1].Lit() != "-v")
	}
	syntax.Walk(tree, func(node syntax.Node) bool {
		function, ok := node.(*syntax.FuncDecl)
		if !ok {
			return true
		}
		syntax.Walk(function.Body, func(node syntax.Node) bool {
			switch node.(type) {
			case *syntax.FuncDecl, *syntax.Redirect:
				return false
			}
			call, ok := node.(*syntax.CallExpr)
			if !ok {
				return true
			}
			if prints(call) {
				for _, arg := range call.Args[1:] {
					syntax.Walk(arg, func(node syntax.Node) bool {
						if _, ok := node.(*syntax.CmdSubst); ok {
							return false
						}
						if param, ok := node.(*syntax.ParamExp); ok {
							text := string(source[param.Pos().Offset():param.End().Offset()])
							if text == "$1" || text == "${1}" {
								printers[function.Name.Value] = true
							}
							return false
						}
						return true
					})
				}
			}
			return false
		})
		return true
	})
	var messages []example
	syntax.Walk(tree, func(node syntax.Node) bool {
		if _, ok := node.(*syntax.Redirect); ok {
			return false
		}
		call, ok := node.(*syntax.CallExpr)
		if !ok {
			return true
		}
		if len(call.Args) < 2 {
			return false
		}
		name := call.Args[0].Lit()
		printed := printers[name]
		if name == "printf" && !prints(call) {
			return false
		}
		for i, arg := range call.Args[1:] {
			if !printed || name != "echo" && name != "printf" && i != 0 || len(arg.Parts) != 1 {
				continue
			}
			text := string(source[arg.Pos().Offset():arg.End().Offset()])
			if len(text) < 2 || text[0] != '\'' && text[0] != '"' || text[len(text)-1] != text[0] {
				continue
			}
			value := text[1 : len(text)-1]
			if quoted, ok := arg.Parts[0].(*syntax.DblQuoted); ok && slices.ContainsFunc(quoted.Parts, func(part syntax.WordPart) bool { _, literal := part.(*syntax.Lit); return !literal }) {
				continue
			}
			if arg.Pos().Line() != call.Pos().Line() || strings.Contains(value, "\n") {
				continue
			}
			values := []string{value}
			if name == "printf" && i == 0 {
				values = strings.Split(strings.NewReplacer(`\\`, `\`, `\n`, "\n", `\t`, "\t").Replace(value), "\n")
			}
			for _, value := range values {
				messages = append(messages, example{value, int(arg.Pos().Line())})
			}
		}
		return false
	})
	return messages, nil
}

// visible returns the text a reader sees in node, so emphasis around the note still counts.
func visible(node ast.Node, source []byte) string {
	var words strings.Builder
	_ = ast.Walk(node, func(node ast.Node, entering bool) (ast.WalkStatus, error) {
		if text, ok := node.(*ast.Text); ok && entering {
			words.WriteString(text.Value.Value(source))
		}
		return ast.WalkContinue, nil
	})
	return words.String()
}

func examples(file string, source []byte, binaries []string) ([]exampleBlock, error) {
	markdown := slices.Contains(markdownExtensions, filepath.Ext(file))
	var result []exampleBlock
	var current exampleBlock
	add := func(value string, line int) bool {
		value = strings.TrimSpace(strings.TrimPrefix(strings.TrimSpace(value), "$ "))
		// Text that doesn't parse as a shell command is prose.
		invocations, err := commands(value)
		if err == nil && slices.ContainsFunc(invocations, func(args []string) bool { return slices.Contains(binaries, args[0]) }) {
			current.commands = append(current.commands, example{text: value, line: line})
			return true
		}
		return false
	}
	if !markdown {
		messages, err := installerStrings(source, filepath.Ext(file) == ".ps1")
		if err != nil {
			return nil, fmt.Errorf("%s:%w", file, err)
		}
		sourceLines := strings.Split(string(source), "\n")
		markerFor := func(line int) (string, int) {
			for line > 1 && line-1 <= len(sourceLines) {
				previous := strings.TrimSpace(sourceLines[line-2])
				if previous == "" {
					break
				}
				if strings.HasPrefix(previous, "#") {
					switch {
					case strings.Contains(previous, note):
						return "unreleased note", line - 1
					case strings.Contains(previous, "release-commands: expected-failure"):
						return "expected-failure marker", line - 1
					case strings.Contains(previous, "release-commands: prose"):
						return "prose", line - 1
					}
				}
				line--
			}
			return "", 0
		}
		flush := func() {
			if len(current.commands) > 0 {
				result = append(result, current)
			}
			current = exampleBlock{}
		}
		blockLine := -1
		for _, message := range messages {
			marker, key := markerFor(message.line)
			if key != blockLine {
				flush()
				current.marker = marker
				blockLine = key
			}
			if marker == "prose" {
				continue
			}
			for candidate, more := message.text, true; more; _, candidate, more = strings.Cut(candidate, ":") {
				if add(candidate, message.line) {
					break
				}
			}
		}
		flush()
		return result, nil
	}
	root := parser.New(parser.WithExtensions(extension.NewTableParser())).Parse(source)
	var collect func(ast.Node, string) []exampleBlock
	collect = func(parent ast.Node, inherited string) []exampleBlock {
		var blocks []exampleBlock
		pending := ""
		for child := parent.FirstChild(); child != nil; child = child.NextSibling() {
			if html, ok := child.(*ast.HTMLBlock); ok && strings.Contains(html.Value.Str(source), "<!-- release-commands: expected-failure -->") {
				pending = "expected-failure marker"
				continue
			}
			if child.Kind() == ast.KindParagraph && strings.HasPrefix(visible(child, source), note) {
				pending = "unreleased note"
				continue
			}
			marker := cmp.Or(inherited, pending)
			switch child.(type) {
			case *ast.List, *ast.ListItem, *ast.Blockquote:
				nested := collect(child, marker)
				if marker == "" {
					blocks = append(blocks, nested...)
					break
				}
				merged := exampleBlock{marker: marker}
				for _, block := range nested {
					merged.commands = append(merged.commands, block.commands...)
				}
				if len(merged.commands) > 0 {
					blocks = append(blocks, merged)
				}
			default:
				current = exampleBlock{marker: marker}
				_ = ast.Walk(child, func(node ast.Node, entering bool) (ast.WalkStatus, error) {
					if code, ok := node.(*ast.CodeBlock); ok && entering {
						pending, start := "", 0
						segments := code.Value.Segments()
						prompted := false
						for _, segment := range segments {
							prompted = prompted || strings.HasPrefix(strings.TrimSpace(string(segment.Bytes(source))), "$ ")
						}
						for index, segment := range segments {
							value := strings.TrimRight(string(segment.Bytes(source)), "\r\n")
							if pending == "" {
								if prompted && !strings.HasPrefix(strings.TrimSpace(value), "$ ") {
									continue
								}
								start = bytes.Count(source[:segment.Start], []byte{'\n'}) + 1
								pending = strings.TrimPrefix(strings.TrimLeft(value, " \t"), "$ ")
							} else if strings.HasSuffix(pending, "\n") {
								pending += value
							} else {
								pending += strings.TrimSpace(value)
							}
							if commentIndex(pending) == len(pending) && (len(pending)-len(strings.TrimRight(pending, "\\")))%2 == 1 {
								pending = strings.TrimSuffix(pending, "\\") + " "
								continue
							}
							if _, err := commands(pending); syntax.IsIncomplete(err) {
								pending += "\n"
								continue
							}
							if index+1 < len(segments) {
								following := string(segments[index+1].Bytes(source))
								if strings.HasPrefix(strings.TrimLeft(following, " \t"), "[") && len(following) > len(strings.TrimLeft(following, " \t")) {
									pending = pending[:commentIndex(pending)] + " "
									continue
								}
							}
							add(pending, start)
							pending = ""
						}
						if pending != "" {
							add(pending, start)
						}
						return ast.WalkSkipChildren, nil
					}
					if span, ok := node.(*ast.CodeSpan); ok && entering {
						position := span.Pos()
						// Goldmark owns decoded table text; its cell retains the source position.
						for parent := span.Parent(); position < 0 && parent != nil; parent = parent.Parent() {
							position = parent.Pos()
						}
						add(span.Value.Value(source), bytes.Count(source[:position], []byte{'\n'})+1)
					}
					return ast.WalkContinue, nil
				})
				if len(current.commands) > 0 {
					blocks = append(blocks, current)
				}
			}
			pending = ""
		}
		return blocks
	}
	return collect(root, ""), nil
}
