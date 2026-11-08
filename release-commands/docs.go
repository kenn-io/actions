package main

import (
	"bytes"
	"cmp"
	"context"
	_ "embed"
	"encoding/json/v2"
	"fmt"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"time"

	"github.com/yuin/goldmark/v2/ast"
	"github.com/yuin/goldmark/v2/extension"
	"github.com/yuin/goldmark/v2/parser"
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

//go:embed installer.ps1
var powershellParser string

func installerStrings(source string) ([]example, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, "pwsh", "-NoProfile", "-NonInteractive", "-Command", powershellParser)
	cmd.Stdin = strings.NewReader(strings.ReplaceAll(source, "\r\n", "\n"))
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	data, err := cmd.Output()
	if err != nil {
		return nil, fmt.Errorf("PowerShell parser: %w: %s", err, strings.TrimSpace(stderr.String()))
	}
	var messages []struct {
		Text string `json:"text"`
		Line int    `json:"line"`
	}
	if err := json.Unmarshal(data, &messages); err != nil {
		return nil, err
	}
	var result []example
	for _, message := range messages {
		result = append(result, example{message.Text, message.Line})
	}
	return result, nil
}

const note = "Not in the latest release yet"

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
	add := func(value string, line int) {
		value = strings.TrimSpace(strings.TrimPrefix(strings.TrimSpace(value), "$ "))
		// An installer message that doesn't parse as a shell command is prose.
		invocations, err := commands(value)
		if err != nil && filepath.Ext(file) != ".md" {
			return
		}
		for _, binary := range binaries {
			words := strings.Fields(value)
			if len(words) > 0 && words[0] == binary || slices.ContainsFunc(invocations, func(args []string) bool { return args[0] == binary }) {
				current.commands = append(current.commands, example{text: value, line: line})
				break
			}
		}
	}
	if filepath.Ext(file) != ".md" {
		var messages []example
		var failure error
		if filepath.Ext(file) == ".ps1" {
			messages, failure = installerStrings(string(source))
		} else if tree, err := syntax.NewParser().Parse(bytes.NewReader(source), ""); err != nil {
			failure = err
		} else {
			printers := map[string]bool{"echo": true, "printf": true}
			// Follow installer helpers to the output calls they wrap.
			for changed := true; changed; {
				changed = false
				syntax.Walk(tree, func(node syntax.Node) bool {
					function, ok := node.(*syntax.FuncDecl)
					if !ok || printers[function.Name.Value] {
						return true
					}
					syntax.Walk(function.Body, func(node syntax.Node) bool {
						if call, ok := node.(*syntax.CallExpr); ok && len(call.Args) > 0 && printers[call.Args[0].Lit()] {
							printers[function.Name.Value], changed = true, true
						}
						return true
					})
					return false
				})
			}
			syntax.Walk(tree, func(node syntax.Node) bool {
				if _, substitution := node.(*syntax.CmdSubst); substitution {
					return false
				}
				call, ok := node.(*syntax.CallExpr)
				if !ok || len(call.Args) == 0 {
					return true
				}
				name, _ := expand.Literal(shellExpansion, call.Args[0])
				if !printers[name] {
					return true
				}
				if len(call.Args) > 1 && (name == "printf" || name == "echo") {
					var args []string
					for _, word := range call.Args[1:] {
						value, err := expand.Literal(shellExpansion, word)
						if err != nil {
							failure = cmp.Or(failure, err)
							return false
						}
						args = append(args, value)
					}
					if name == "echo" {
						messages = append(messages, example{strings.Join(args, " "), int(call.Args[1].Pos().Line())})
						return true
					}
					if args[0] == "--" && len(args) > 1 {
						args = args[1:]
					}
					format, args := args[0], args[1:]
					for {
						value, used, err := expand.Format(shellExpansion, format, args)
						if err != nil {
							failure = cmp.Or(failure, err)
							break
						}
						for line := range strings.SplitSeq(value, "\n") {
							messages = append(messages, example{line, int(call.Args[1].Pos().Line())})
						}
						args = args[used:]
						if used == 0 || len(args) == 0 {
							break
						}
					}
					return true
				}
				for _, word := range call.Args[1:] {
					if len(word.Parts) != 1 {
						continue
					}
					switch word.Parts[0].(type) {
					case *syntax.SglQuoted, *syntax.DblQuoted:
						value, err := expand.Literal(shellExpansion, word)
						failure = cmp.Or(failure, err)
						if err == nil {
							messages = append(messages, example{value, int(word.Pos().Line())})
						}
					}
				}
				return true
			})
		}
		if failure != nil {
			return nil, fmt.Errorf("%s: cannot read installer messages: %w", file, failure)
		}
		for _, message := range messages {
			for offset, value := range strings.Split(message.text, "\n") {
				current = exampleBlock{}
				for {
					add(value, message.line+offset)
					_, rest, found := strings.Cut(value, ":")
					if !found {
						break
					}
					value = rest
				}
				if len(current.commands) > 0 {
					result = append(result, current)
				}
			}
		}
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
			if html, ok := markerNode.(*ast.HTMLBlock); ok {
				if strings.Contains(html.Value.Str(source), "<!-- release-commands: expected-failure -->") {
					pending = "expected-failure marker"
				}
				if pending != "" {
					continue
				}
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
							}
							pending += strings.TrimSpace(value)
							if prefix, ok := strings.CutSuffix(pending, "\\"); ok {
								pending = prefix + " "
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
