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
	"go.kenn.io/kit/tui/termtext"
	"mvdan.cc/sh/v3/expand"
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
	installerTokens    = regexp.MustCompile(`(?s)'(?:''|[^'])*'|"(?:\\.|` + "`" + `.|""|[^"\\` + "`" + `])*"|<\#.*?\#>|\#[^\n]*|[;{}\n]|[^\s;{}'"\#]+`)
	shellFunction      = regexp.MustCompile(`\b([\w-]+)\s*\(\s*\)\s*\{`)
	powershellFunction = regexp.MustCompile(`(?i)\bfunction\s+([\w-]+)`)
	colorPrefix        = regexp.MustCompile(`^(?:\s*\$(?:\{\w+\}|\w+))+`)
)

// installerStrings reads single-line quoted output arguments throughout the source.
func installerStrings(source []byte, powershell bool) []example {
	if !powershell {
		if tree, err := syntax.NewParser().Parse(bytes.NewReader(source), ""); err == nil {
			source = append([]byte(nil), source...)
			syntax.Walk(tree, func(node syntax.Node) bool {
				if redirect, ok := node.(*syntax.Redirect); ok && redirect.Hdoc != nil {
					for i := redirect.Hdoc.Pos().Offset(); i < redirect.Hdoc.End().Offset(); i++ {
						if source[i] != '\n' {
							source[i] = ' '
						}
					}
				}
				return true
			})
		}
	}
	matches := installerTokens.FindAllIndex(source, -1)
	code := append([]byte(nil), source...)
	for _, match := range matches {
		if strings.ContainsRune("'\"#<", rune(source[match[0]])) {
			copy(code[match[0]:match[1]], bytes.Repeat([]byte{' '}, match[1]-match[0]))
		}
	}
	// Each function body opens at its first brace; a body that calls a printer makes a wrapper.
	printers, bodies := map[string]bool{"echo": true, "printf": true}, map[int]string{}
	for _, pattern := range []*regexp.Regexp{shellFunction, powershellFunction} {
		for _, match := range pattern.FindAllSubmatchIndex(code, -1) {
			name := string(code[match[2]:match[3]])
			if powershell {
				name = strings.ToLower(name)
			}
			bodies[match[0]+bytes.IndexByte(code[match[0]:], '{')] = name
		}
	}
	var messages []example
	// Rescan until no new wrapper appears, so declaration order doesn't matter.
	for known := 0; known != len(printers); {
		known, messages = len(printers), nil
		call, boundary, escapes, argument, metadata, owners := "", true, false, 0, false, []string{}
		for _, match := range matches {
			text := string(source[match[0]:match[1]])
			if strings.HasPrefix(text, "#") || strings.HasPrefix(text, "<#") {
				continue
			}
			if text == "{" {
				owners = append(owners, bodies[match[0]])
			} else if text == "}" && len(owners) > 0 {
				owners = owners[:len(owners)-1]
			}
			operator := text == "&&" || text == "||" || text == "|" || !powershell && text == "&"
			if strings.ContainsAny(text, ";{}\n") && len(text) == 1 || operator || boundary && !powershell && (text == "then" || text == "else" || text == "do" || strings.HasSuffix(text, ")")) {
				call, boundary, escapes, argument, metadata = "", true, false, 0, false
				continue
			}
			if boundary {
				name := text
				if powershell {
					name = strings.ToLower(name)
				}
				if printers[name] || powershell && strings.HasPrefix(name, "write-") {
					call = name
					for _, owner := range owners {
						printers[owner] = true
					}
				}
				boundary = false
				continue
			}
			// Unquoted words never span lines, so only quoted multiline strings stop here.
			if call == "" || strings.Contains(text, "\n") {
				continue
			}
			if text[0] != '\'' && text[0] != '"' {
				if call == "printf" && !strings.HasPrefix(text, "-") && !strings.ContainsAny(text, "<>") {
					argument++
				}
				name := strings.ToLower(text)
				metadata = powershell && strings.HasPrefix(name, "-") && !strings.Contains(name, ":") && !slices.Contains([]string{"-object", "-message", "-messagedata", "-nonewline", "-noenumerate"}, name)
				if strings.Contains(text, "(") || call == "printf" && text == "-v" {
					call = ""
				}
				if call == "echo" && strings.HasPrefix(text, "-") {
					if option := strings.LastIndexAny(text, "eE"); option > 0 {
						escapes = text[option] == 'e'
					}
				}
				continue
			}
			value := colorPrefix.ReplaceAllString(text[1:len(text)-1], "")
			if metadata || text[0] == '"' && expands(value, powershell) {
				argument++
				metadata = false
				continue
			}
			switch {
			case powershell && text[0] == '\'':
				value = strings.ReplaceAll(value, "''", "'")
			case powershell:
				value = strings.ReplaceAll(strings.NewReplacer("`n", "\n", "`t", "\t", "`e", "\x1b", "`\"", "\"", "``", "`", "`$", "$").Replace(value), "\"\"", "\"")
			case text[0] == '"':
				value = strings.NewReplacer(`\\`, `\`, `\"`, `"`, `\$`, `$`, "\\`", "`").Replace(value)
			}
			if !powershell && (call == "printf" && argument == 0 || escapes) {
				value, _, _ = expand.Format(nil, strings.ReplaceAll(value, "%", "%%"), nil)
			}
			argument++
			messages = append(messages, example{value, bytes.Count(source[:match[0]], []byte{'\n'}) + 1})
		}
	}
	return messages
}

func expands(value string, powershell bool) bool {
	for i := 0; i < len(value); i++ {
		if !powershell && value[i] == '\\' || powershell && value[i] == '`' {
			i++
			continue
		}
		if value[i] == '$' || !powershell && value[i] == '`' {
			return true
		}
	}
	return false
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
	markdown := slices.Contains([]string{".md", ".mdx", ".markdown"}, filepath.Ext(file))
	if !markdown && filepath.Ext(file) != ".sh" && filepath.Ext(file) != ".ps1" {
		return nil, fmt.Errorf("%s: unsupported file type", file)
	}
	var result []exampleBlock
	var current exampleBlock
	add := func(value string, line int) bool {
		value = strings.TrimSpace(strings.TrimPrefix(strings.TrimSpace(value), "$ "))
		// An installer message that doesn't parse as a shell command is prose.
		invocations, err := commands(value)
		if err != nil && !markdown {
			return false
		}
		if slices.Contains(binaries, commandName(value)) || slices.ContainsFunc(invocations, func(args []string) bool { return slices.Contains(binaries, args[0]) }) {
			current.commands = append(current.commands, example{text: value, line: line})
			return true
		}
		return false
	}
	if !markdown {
		messages := installerStrings(source, filepath.Ext(file) == ".ps1")
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
			for value := range strings.SplitSeq(termtext.StripANSI(message.text), "\n") {
				line := message.line
				marker, key := markerFor(line)
				if key != blockLine {
					flush()
					current.marker = marker
					blockLine = key
				}
				for found := true; found; _, value, found = strings.Cut(value, ":") {
					if add(value, line) {
						break
					}
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
			markerNode := child
			for {
				quote, ok := markerNode.(*ast.Blockquote)
				if !ok || quote.ChildCount() != 1 {
					break
				}
				markerNode = quote.FirstChild()
			}
			if html, ok := markerNode.(*ast.HTMLBlock); ok && strings.Contains(html.Value.Str(source), "<!-- release-commands: expected-failure -->") {
				pending = "expected-failure marker"
				continue
			}
			if markerNode.Kind() == ast.KindParagraph && strings.HasPrefix(visible(markerNode, source), note) {
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
						for index, segment := range segments {
							value := strings.TrimRight(string(segment.Bytes(source)), "\r\n")
							if pending == "" {
								start = bytes.Count(source[:segment.Start], []byte{'\n'}) + 1
								pending = strings.TrimLeft(value, " \t")
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
									pending += " "
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
