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
	Comment   string `json:"comment"`
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

// leading returns the whole source lines between stmt and the code before it, or the earlier
// output end after, so comments, blank lines, and silent statements read like a top-level gap.
func leading(source []byte, after int, stmt *syntax.Stmt) string {
	start := stmt.Pos().Offset()
	if len(stmt.Comments) > 0 {
		start = min(start, stmt.Comments[0].Pos().Offset())
	}
	_, lines, _ := strings.Cut(string(source[min(after, len(bytes.TrimRight(source[:start], " \t\r\n"))):stmt.Pos().Offset()]), "\n")
	return lines
}

func shellStrings(source []byte) ([]installerMessage, error) {
	tree, err := syntax.NewParser(syntax.KeepComments(true)).Parse(bytes.NewReader(source), "")
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
	messages, comment, open, emits := []installerMessage{}, "", false, 0
	// Escapes expanded by echo or printf split lines in place; a physical newline, carried
	// as NUL, also moves to the next source line. Unterminated output stays open to continue.
	emit := func(value string, line int, outer *syntax.CallExpr) {
		for index, text := range strings.Split(value, "\n") {
			message := installerMessage{strings.ReplaceAll(text, "\x00", "\n"), line, int(outer.Pos().Offset()), int(outer.End().Offset()), comment}
			line, comment, emits = line+strings.Count(text, "\x00"), "", emits+min(len(value), 1)
			if last := len(messages) - 1; index == 0 && open {
				messages[last].Text, messages[last].End = messages[last].Text+message.Text, message.End
			} else if open = text != ""; open || index < strings.Count(value, "\n") {
				messages = append(messages, message)
			}
		}
	}
	var printCall func(*syntax.CallExpr, shellEnv, int, *syntax.CallExpr) error
	// Helper output keeps its outer call's statement and the comments above each call reaching its printer.
	walk := func(root syntax.Node, env shellEnv, depth int, outer *syntax.CallExpr, inherited string) (failure error) {
		after, count := len(source), emits
		syntax.Walk(root, func(node syntax.Node) bool {
			switch node := node.(type) {
			case *syntax.FuncDecl, *syntax.CmdSubst:
				return false
			case *syntax.Stmt:
				if comment, inherited = inherited, ""; depth > 0 {
					comment += leading(source, after, node)
				}
			case *syntax.CallExpr:
				if failure = cmp.Or(failure, printCall(node, env, depth, cmp.Or(outer, node))); count < emits {
					after, count = int(node.End().Offset()), emits
				}
			}
			return failure == nil
		})
		return failure
	}
	printCall = func(call *syntax.CallExpr, env shellEnv, depth int, outer *syntax.CallExpr) error {
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
			return walk(function.Body.Cmd, shellEnv{args, true}, depth+1, outer, comment)
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
			emit(value, append(args, example{line: int(call.Pos().Line())})[0].line, outer)
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
			emit(value, line, outer)
			values, args = values[used:], args[used:]
			if used == 0 || len(values) == 0 {
				return nil
			}
		}
	}
	err = walk(tree, shellEnv{}, 0, nil, "")
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
			// Empty output is a blank line, which ends a marker block.
			if message.Text == "" {
				flush("")
				continue
			}
			if message.Statement >= position || message.Comment != "" {
				gap := strings.Split(cmp.Or(string(source[min(position, message.Statement):message.Statement]), "\n")+message.Comment, "\n")
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
				for found := true; found; _, value, found = strings.Cut(value, ":") {
					add(value, message.Line+offset)
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
