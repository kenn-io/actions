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
	"strconv"
	"strings"
	"time"

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

//go:embed installer.ps1
var powershellParser string

type installerMessage struct {
	Text      string `json:"text"`
	Line      int    `json:"line"`
	Statement int    `json:"statement"`
	End       int    `json:"end"`
}

func installerStrings(source string) ([]installerMessage, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, "pwsh", "-NoProfile", "-NonInteractive", "-Command", powershellParser)
	cmd.Stdin = strings.NewReader(source)
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	data, err := cmd.Output()
	if err != nil {
		return nil, fmt.Errorf("PowerShell parser: %w: %s", err, strings.TrimSpace(stderr.String()))
	}
	var messages []installerMessage
	if err := json.Unmarshal(data, &messages); err != nil {
		return nil, err
	}
	return messages, nil
}

const note = "Not in the latest release yet"

// shellEnv gives a printer helper its caller's arguments. Other variables
// inside a helper, such as colors, print nothing; outside one they stay visible.
type shellEnv struct {
	args   []example
	helper bool
}

func (env shellEnv) Get(name string) expand.Variable {
	values := make([]string, len(env.args))
	for i, arg := range env.args {
		values[i] = arg.text
	}
	if index, err := strconv.Atoi(name); env.helper && err == nil && index > 0 && index <= len(values) {
		return expand.Variable{Set: true, Kind: expand.String, Str: values[index-1]}
	}
	if env.helper && (name == "@" || name == "*") {
		return expand.Variable{Set: true, Kind: expand.Indexed, List: values}
	}
	if env.helper || name == "IFS" {
		return expand.Variable{}
	}
	return expand.Variable{Set: true, Kind: expand.String, Str: "$" + name}
}

func (shellEnv) Each(func(string, expand.Variable) bool) {}

func shellStrings(source []byte) ([]installerMessage, error) {
	tree, err := syntax.NewParser().Parse(bytes.NewReader(source), "")
	if err != nil {
		return nil, err
	}
	functions := map[string]*syntax.FuncDecl{}
	syntax.Walk(tree, func(node syntax.Node) bool {
		if function, ok := node.(*syntax.FuncDecl); ok {
			functions[function.Name.Value] = function
		}
		return true
	})
	var messages []installerMessage
	// Escapes expanded by echo or printf split lines in place; a physical newline,
	// carried as NUL, also moves to the next source line.
	emit := func(value string, line, statement, end int) {
		for text := range strings.SplitSeq(value, "\n") {
			messages = append(messages, installerMessage{strings.ReplaceAll(text, "\x00", "\n"), line, statement, end})
			line += strings.Count(text, "\x00")
		}
	}
	var printCall func(*syntax.CallExpr, shellEnv, int, int, int) error
	printCall = func(call *syntax.CallExpr, env shellEnv, depth, statement, end int) error {
		if len(call.Args) == 0 {
			return nil
		}
		name := call.Args[0].Lit()
		function := functions[name]
		if name != "echo" && name != "printf" && function == nil {
			return nil
		}
		if depth > len(functions) {
			return fmt.Errorf("recursive printer helper %s", name)
		}
		config := *shellExpansion
		config.Env = env
		var args []example
		for _, word := range call.Args[1:] {
			values, err := expand.Fields(&config, word)
			if err != nil {
				return err
			}
			for _, value := range values {
				line := int(word.Pos().Line())
				for _, caller := range env.args {
					if caller.text != "" && strings.Contains(value, caller.text) {
						line = caller.line
						break
					}
				}
				args = append(args, example{strings.ReplaceAll(value, "\n", "\x00"), line})
			}
		}
		if function != nil {
			var failure error
			syntax.Walk(function.Body, func(node syntax.Node) bool {
				if _, substitution := node.(*syntax.CmdSubst); substitution {
					return false
				}
				if nested, ok := node.(*syntax.CallExpr); ok && failure == nil {
					failure = printCall(nested, shellEnv{args, true}, depth+1, statement, end)
				}
				return failure == nil
			})
			return failure
		}
		escaped := false
		for name == "echo" && len(args) > 0 && len(args[0].text) > 1 && strings.TrimRight(args[0].text, "neE") == "-" {
			if last := strings.LastIndexAny(args[0].text, "eE"); last >= 0 {
				escaped = args[0].text[last] == 'e'
			}
			args = args[1:]
		}
		if len(args) == 0 {
			return nil
		}
		values := make([]string, len(args))
		for i, arg := range args {
			values[i] = arg.text
		}
		if name == "echo" {
			value := strings.Join(values, " ")
			if escaped {
				value, _, err = expand.Format(&config, "%b", []string{value})
			}
			emit(value, args[0].line, statement, end)
			return err
		}
		if values[0] == "--" && len(values) > 1 {
			values, args = values[1:], args[1:]
		}
		format, line := values[0], args[0].line
		values, args = values[1:], args[1:]
		for {
			value, used, err := expand.Format(&config, format, values)
			if err != nil {
				return err
			}
			if used > 0 {
				line = args[0].line
			}
			emit(value, line, statement, end)
			values, args = values[used:], args[used:]
			if used == 0 || len(values) == 0 {
				return nil
			}
		}
	}
	syntax.Walk(tree, func(node syntax.Node) bool {
		switch node.(type) {
		case *syntax.FuncDecl, *syntax.CmdSubst:
			return false
		}
		if call, ok := node.(*syntax.CallExpr); ok && err == nil {
			err = printCall(call, shellEnv{}, 0, int(call.Pos().Offset()), int(call.End().Offset()))
		}
		return err == nil
	})
	return messages, err
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
	add := func(value string, line int) {
		value = strings.TrimSpace(strings.TrimPrefix(strings.TrimSpace(value), "$ "))
		// An installer message that doesn't parse as a shell command is prose.
		invocations, err := commands(value)
		if err != nil && filepath.Ext(file) != ".md" {
			return
		}
		words := strings.Fields(value)
		first := slices.IndexFunc(words, func(word string) bool {
			name, _, assigned := strings.Cut(word, "=")
			return !assigned || !syntax.ValidName(name)
		})
		if first >= 0 && slices.Contains(binaries, words[first]) || slices.ContainsFunc(invocations, func(args []string) bool { return slices.Contains(binaries, args[0]) }) {
			current.commands = append(current.commands, example{text: value, line: line})
		}
	}
	if filepath.Ext(file) != ".md" {
		var messages []installerMessage
		var failure error
		if filepath.Ext(file) == ".ps1" {
			messages, failure = installerStrings(string(source))
		} else {
			messages, failure = shellStrings(source)
		}
		if failure != nil {
			return nil, fmt.Errorf("%s: cannot read installer messages: %w", file, failure)
		}
		flush := func(marker string) {
			if len(current.commands) > 0 {
				result = append(result, current)
			}
			current = exampleBlock{marker: marker}
		}
		position := 0
		for _, message := range messages {
			if message.Text == "" {
				continue
			}
			if message.Statement >= position {
				gap := strings.Split(string(source[position:message.Statement]), "\n")
				for index, line := range gap {
					line = strings.Trim(line, " \t\r;&|")
					switch {
					case strings.HasPrefix(line, "#") && strings.Contains(line, note):
						flush("unreleased note")
					case strings.HasPrefix(line, "#") && strings.Contains(line, "release-commands: expected-failure"):
						flush("expected-failure marker")
					case line == "" && index > 0 && index < len(gap)-1, line != "" && !strings.HasPrefix(line, "#"):
						flush("")
					}
				}
			}
			position = max(position, message.End)
			for offset, value := range strings.Split(termtext.StripANSI(message.Text), "\n") {
				for {
					add(value, message.Line+offset)
					_, rest, found := strings.Cut(value, ":")
					if !found {
						break
					}
					value = rest
				}
			}
		}
		flush("")
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
							}
							pending += strings.TrimSpace(value)
							if (len(pending)-len(strings.TrimRight(pending, "\\")))%2 == 1 {
								pending = strings.TrimSuffix(pending, "\\") + " "
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
