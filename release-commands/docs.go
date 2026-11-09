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

type installerMessage struct {
	Text string
	Line int
}

const note = "Not in the latest release yet"

// Installer readers collect quoted arguments at output calls without evaluating scripts.
func shellStrings(source []byte) ([]installerMessage, error) {
	tree, err := syntax.NewParser().Parse(bytes.NewReader(source), "")
	if err != nil {
		return nil, err
	}
	printers := map[string]bool{"echo": true, "printf": true}
	syntax.Walk(tree, func(node syntax.Node) bool {
		if function, ok := node.(*syntax.FuncDecl); ok {
			syntax.Walk(function.Body, func(node syntax.Node) bool {
				if call, ok := node.(*syntax.CallExpr); ok && len(call.Args) > 0 && printers[call.Args[0].Lit()] {
					printers[function.Name.Value] = true
				}
				return true
			})
			return false
		}
		return true
	})
	var messages []installerMessage
	syntax.Walk(tree, func(node syntax.Node) bool {
		if _, ok := node.(*syntax.CmdSubst); ok {
			return false
		}
		call, ok := node.(*syntax.CallExpr)
		if !ok || len(call.Args) == 0 || !printers[call.Args[0].Lit()] {
			return true
		}
		if call.Args[0].Lit() == "printf" && len(call.Args) > 1 && call.Args[1].Lit() == "-v" {
			return false
		}
		for _, word := range call.Args[1:] {
			if len(word.Parts) != 1 {
				continue
			}
			switch part := word.Parts[0].(type) {
			case *syntax.SglQuoted:
				messages = append(messages, installerMessage{part.Value, int(part.Pos().Line())})
			case *syntax.DblQuoted:
				text := string(source[part.Pos().Offset()+1 : part.End().Offset()-1])
				if !strings.Contains(text, "$(") && !strings.Contains(text, "`") {
					messages = append(messages, installerMessage{text, int(part.Pos().Line())})
				}
			}
		}
		return false
	})
	return messages, nil
}

var (
	powershellFunction = regexp.MustCompile(`(?is)\bfunction\s+([\w-]+)[^{]*\{[^}]*\bWrite-[\w-]+[^}]*\}`)
	powershellCall     = regexp.MustCompile(`^\s*([\w-]+)\s+`)
	colorPrefix        = regexp.MustCompile(`^\s*\$(?:\{[\w]+\}|[\w]+)`)
)

func powershellStrings(source []byte) ([]installerMessage, error) {
	// Remove comments while preserving positions and quoted strings.
	clean := append([]byte(nil), source...)
	codeLines := []bool{true}
	var quote byte
	block := false
	for i := 0; i < len(clean); i++ {
		if clean[i] == '\n' {
			codeLines = append(codeLines, quote == 0 && !block)
		}
		if block {
			if i+1 < len(clean) && clean[i] == '#' && clean[i+1] == '>' {
				clean[i], clean[i+1], block = ' ', ' ', false
				i++
			} else if clean[i] != '\n' {
				clean[i] = ' '
			}
			continue
		}
		if quote != 0 {
			if clean[i] == '`' && quote == '"' {
				if i+1 < len(clean) && clean[i+1] == '\n' {
					codeLines = append(codeLines, false)
				}
				i++
			} else if clean[i] == quote {
				if i+1 < len(clean) && clean[i+1] == quote {
					i++
				} else {
					quote = 0
				}
			}
			continue
		}
		switch clean[i] {
		case '\'', '"':
			quote = clean[i]
		case '<':
			if i+1 < len(clean) && clean[i+1] == '#' {
				clean[i], clean[i+1], block = ' ', ' ', true
				i++
			}
		case '#':
			for i < len(clean) && clean[i] != '\n' {
				clean[i] = ' '
				i++
			}
			if i < len(clean) {
				codeLines = append(codeLines, true)
			}
		}
	}
	lines := strings.Split(string(clean), "\n")
	printers := map[string]bool{}
	for _, match := range powershellFunction.FindAllStringSubmatch(string(clean), -1) {
		printers[strings.ToLower(match[1])] = true
	}
	var messages []installerMessage
	for line, text := range lines {
		if !codeLines[line] {
			continue
		}
		call := powershellCall.FindStringSubmatchIndex(text)
		if call == nil {
			continue
		}
		name := strings.ToLower(text[call[2]:call[3]])
		if !strings.HasPrefix(name, "write-") && !printers[name] {
			continue
		}
		for i := call[1]; i < len(text); i++ {
			if text[i] == ';' || text[i] == '(' {
				break
			}
			if text[i] != '\'' && text[i] != '"' {
				continue
			}
			quote, start := text[i], i+1
			i++
			for i < len(text) && text[i] != quote {
				if text[i] == '`' && quote == '"' {
					i++
				}
				i++
			}
			if i < len(text) && !strings.Contains(text[start:i], "$(") {
				messages = append(messages, installerMessage{text[start:i], line + 1})
			}
		}
	}
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
	var result []exampleBlock
	var current exampleBlock
	add := func(value string, line int) bool {
		value = strings.TrimSpace(strings.TrimPrefix(strings.TrimSpace(value), "$ "))
		// An installer message that doesn't parse as a shell command is prose.
		invocations, err := commands(value)
		if err != nil && filepath.Ext(file) != ".md" {
			return false
		}
		if slices.Contains(binaries, commandName(value)) || slices.ContainsFunc(invocations, func(args []string) bool { return slices.Contains(binaries, args[0]) }) {
			current.commands = append(current.commands, example{text: value, line: line})
			return true
		}
		return false
	}
	if filepath.Ext(file) != ".md" {
		read := shellStrings
		if filepath.Ext(file) == ".ps1" {
			read = powershellStrings
		}
		messages, err := read(source)
		if err != nil {
			return nil, fmt.Errorf("%s: cannot read installer messages: %w", file, err)
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
			text := message.Text
			for colorPrefix.MatchString(text) {
				text = colorPrefix.ReplaceAllString(text, "")
			}
			for offset, value := range strings.Split(termtext.StripANSI(text), "\n") {
				line := message.Line
				if filepath.Ext(file) != ".ps1" {
					line += offset
				}
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
