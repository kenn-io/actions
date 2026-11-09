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

type installerMessage struct {
	Text string
	Line int
}

//go:embed installer.ps1
var powershellReader string

func powershellStrings(source []byte) ([]installerMessage, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, "pwsh", "-NoProfile", "-NonInteractive", "-Command", powershellReader)
	cmd.Stdin = bytes.NewReader(source)
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	data, err := cmd.Output()
	if err != nil {
		return nil, fmt.Errorf("PowerShell parser: %w: %s", err, strings.TrimSpace(stderr.String()))
	}
	var messages []installerMessage
	return messages, json.Unmarshal(data, &messages)
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
	var pending installerMessage
	flush := func() {
		if pending.Text != "" {
			pending.Text = strings.ReplaceAll(pending.Text, "\x00", "\n")
			messages = append(messages, pending)
		}
		pending = installerMessage{}
	}
	// Escaped output newlines split messages; NULs mark physical source newlines.
	emit := func(value string, line int) {
		for {
			text, rest, newline := strings.Cut(value, "\n")
			if pending.Text == "" {
				pending.Line = line
			}
			pending.Text += text
			line += strings.Count(text, "\x00")
			if !newline {
				break
			}
			flush()
			value = rest
		}
	}
	var printCall func(*syntax.CallExpr, shellEnv, int) error
	walk := func(root syntax.Node, env shellEnv, depth int) error {
		var failure error
		syntax.Walk(root, func(node syntax.Node) bool {
			switch node := node.(type) {
			case *syntax.FuncDecl, *syntax.CmdSubst:
				return false
			case *syntax.CallExpr:
				failure = printCall(node, env, depth)
			}
			return failure == nil
		})
		return failure
	}
	printCall = func(call *syntax.CallExpr, env shellEnv, depth int) error {
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
		args, values, flags := []example{}, []string{}, ""
		for _, word := range call.Args[1:] {
			fields, err := expand.Fields(&config, word)
			if err != nil {
				return err
			}
			for _, value := range fields {
				line := int(word.Pos().Line())
				if caller := slices.IndexFunc(env.args, func(arg example) bool { return arg.text != "" && strings.Contains(value, arg.text) }); caller >= 0 {
					line = env.args[caller].line
				}
				args = append(args, example{strings.ReplaceAll(value, "\n", "\x00"), line})
				values = append(values, args[len(args)-1].text)
			}
		}
		if function != nil {
			return walk(function.Body.Cmd, shellEnv{args, true}, depth+1)
		}
		for name == "echo" && len(args) > 0 && len(args[0].text) > 1 && strings.TrimRight(args[0].text, "neE") == "-" {
			flags, args, values = flags+args[0].text, args[1:], values[1:]
		}
		if len(args) == 0 && name != "echo" {
			return nil
		}
		if name == "echo" {
			value := strings.Join(values, " ")
			if !strings.Contains(flags, "n") {
				value += "\n"
			}
			// The last of -e and -E wins.
			if strings.HasSuffix(strings.TrimRight(flags, "-n"), "e") {
				value, _, err = expand.Format(&config, "%b", []string{value})
			}
			emit(value, append(args, example{line: int(call.Pos().Line())})[0].line)
			return err
		}
		if values[0] == "--" && len(values) > 1 {
			values, args = values[1:], args[1:]
		}
		format, line, values, args := values[0], args[0].line, values[1:], args[1:]
		for {
			value, used, err := expand.Format(&config, format, values)
			if err != nil {
				return err
			}
			if used > 0 {
				line = args[0].line
			}
			emit(value, line)
			values, args = values[used:], args[used:]
			if used == 0 || len(values) == 0 {
				return nil
			}
		}
	}
	err = walk(tree, shellEnv{}, 0)
	flush()
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
			for offset, value := range strings.Split(termtext.StripANSI(message.Text), "\n") {
				line := message.Line + offset
				marker, key := markerFor(line)
				if key != blockLine {
					flush()
					current.marker = marker
					blockLine = key
				}
				for found := true; found; _, value, found = strings.Cut(value, ":") {
					add(value, line)
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
							if !comment(pending) && (len(pending)-len(strings.TrimRight(pending, "\\")))%2 == 1 {
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
