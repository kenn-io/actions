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
	"mvdan.cc/sh/v3/expand"
	"mvdan.cc/sh/v3/syntax"
)

type example struct {
	text        string
	line        int
	invocations [][]string
}
type exampleBlock struct {
	commands []example
	marker   string
}

const note = "Not in the latest release yet"

var (
	markdownExtensions = []string{".md", ".mdx", ".markdown"}
	powershellWrites   = regexp.MustCompile(`(?im)(?:^|[;{}|(&])[ \t]*write-[\w-]+[ \t]+('(?:''|[^'\r\n])*'|"[^"\r\n]*")`)
)

// installerStrings reads static, single-line output and one-step message helpers.
func installerStrings(source []byte, powershell bool) ([]example, error) {
	if !powershell {
		return shellStrings(source)
	}
	var messages []example
	for _, match := range powershellWrites.FindAllSubmatchIndex(source, -1) {
		text := string(source[match[2]:match[3]])
		value := text[1 : len(text)-1]
		if text[0] == '\'' {
			value = strings.ReplaceAll(value, "''", "'")
		}
		if text[0] == '\'' || !strings.ContainsAny(value, "$`") {
			messages = append(messages, example{text: value, line: bytes.Count(source[:match[2]], []byte{'\n'}) + 1})
		}
	}
	return messages, nil
}

// shellStrings renders static printer calls and one-step message helpers.
func shellStrings(source []byte) ([]example, error) {
	tree, err := syntax.NewParser().Parse(bytes.NewReader(source), "")
	if err != nil {
		return nil, err
	}
	helpers := map[string][]*syntax.CallExpr{}
	probe := "\x00"
	syntax.Walk(tree, func(node syntax.Node) bool {
		function, ok := node.(*syntax.FuncDecl)
		if !ok {
			return true
		}
		syntax.Walk(function.Body, func(node syntax.Node) bool {
			if _, ok := node.(*syntax.FuncDecl); ok {
				return false
			}
			call, ok := node.(*syntax.CallExpr)
			if !ok {
				return true
			}
			args, static := shellWords(call, source, &probe)
			if output, printed := shellOutput(args); static && printed && strings.Contains(output, probe) {
				helpers[function.Name.Value] = append(helpers[function.Name.Value], call)
			}
			return false
		})
		return true
	})
	var messages []example
	syntax.Walk(tree, func(node syntax.Node) bool {
		call, ok := node.(*syntax.CallExpr)
		if !ok {
			return true
		}
		args, static := shellWords(call, source, nil)
		if !static || len(args) == 0 {
			return false
		}
		calls, parameter := []*syntax.CallExpr{call}, (*string)(nil)
		if len(args) > 1 && len(helpers[args[0]]) > 0 {
			calls, parameter = helpers[args[0]], &args[1]
		}
		for _, printer := range calls {
			words, static := args, true
			if parameter != nil {
				words, static = shellWords(printer, source, parameter)
			}
			if output, printed := shellOutput(words); static && printed {
				for text := range strings.SplitSeq(output, "\n") {
					messages = append(messages, example{text: text, line: int(call.Pos().Line())})
				}
			}
		}
		return false
	})
	return messages, nil
}

// shellWords accepts literal words, or an unchanged first helper parameter.
func shellWords(call *syntax.CallExpr, source []byte, parameter *string) ([]string, bool) {
	config := &expand.Config{}
	if parameter != nil {
		config.Env = expand.ListEnviron("1=" + *parameter)
	}
	var args []string
	for _, word := range call.Args {
		static := true
		syntax.Walk(word, func(node syntax.Node) bool {
			switch node := node.(type) {
			case nil, *syntax.Word, *syntax.Lit, *syntax.SglQuoted, *syntax.DblQuoted:
				return true
			case *syntax.ParamExp:
				text := string(source[node.Pos().Offset():node.End().Offset()])
				static = static && parameter != nil && (text == "$1" || text == "${1}")
			default:
				static = false
			}
			return false
		})
		if !static {
			return nil, false
		}
		values, err := expand.Fields(config, word)
		if err != nil {
			return nil, false
		}
		args = append(args, values...)
	}
	return args, true
}

func shellOutput(words []string) (string, bool) {
	if len(words) < 2 {
		return "", false
	}
	switch words[0] {
	case "echo":
		return strings.Join(words[1:], " "), true
	case "printf":
		output, _, err := expand.Format(nil, words[1], words[2:])
		return output, err == nil
	}
	return "", false
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
	if _, text, ok := strings.Cut(words.String(), "]"); ok && strings.HasPrefix(words.String(), "[!") {
		return strings.TrimSpace(text)
	}
	return words.String()
}

func examples(file string, source []byte, binaries []string) ([]exampleBlock, error) {
	markdown := slices.Contains(markdownExtensions, strings.ToLower(filepath.Ext(file)))
	var result []exampleBlock
	var current exampleBlock
	add := func(value string, line int) bool {
		value = strings.TrimSpace(strings.TrimPrefix(strings.TrimSpace(value), "$ "))
		// Text that doesn't parse as a shell command is prose.
		invocations, err := commands(value)
		if err == nil && len(invocations) > 0 && (markdown || slices.Contains(binaries, invocations[0][0])) && slices.ContainsFunc(invocations, func(args []string) bool { return slices.Contains(binaries, args[0]) }) {
			current.commands = append(current.commands, example{text: value, line: line, invocations: invocations})
			return true
		}
		return false
	}
	if !markdown {
		messages, err := installerStrings(source, strings.EqualFold(filepath.Ext(file), ".ps1"))
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
				if strings.HasPrefix(previous, "#") && strings.Contains(previous, note) {
					return "unreleased note", line - 1
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
			before := len(blocks)
			if html, ok := child.(*ast.HTMLBlock); ok && strings.Contains(html.Value.Str(source), "<!-- release-commands: expected-failure -->") {
				pending = "expected-failure marker"
				continue
			}
			markerNode := child
			if child.Kind() == ast.KindBlockquote && child.FirstChild() == child.LastChild() && child.FirstChild() != nil {
				markerNode = child.FirstChild()
			}
			noteParagraph := markerNode.Kind() == ast.KindParagraph && strings.HasPrefix(visible(markerNode, source), note)
			if noteParagraph {
				pending = "unreleased note"
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
			if !noteParagraph || len(blocks) > before {
				pending = ""
			}
		}
		return blocks
	}
	return collect(root, ""), nil
}
