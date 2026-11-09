package main

import (
	"bytes"
	"cmp"
	"fmt"
	"path/filepath"
	"regexp"
	"slices"
	"strconv"
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
	installerTokens      = regexp.MustCompile(`(?s)'(?:''|[^'])*'|"(?:\\.|` + "`" + `.|""|[^"\\` + "`" + `])*"|<\#.*?\#>|\#[^\n]*|[;{}\n]|[^\s;{}'"\#]+`)
	powershellFunction   = regexp.MustCompile(`(?i)\bfunction\s+([\w-]+)`)
	colorPrefix          = regexp.MustCompile(`^(?:\s*\$(?:\{\w+\}|\w+))+`)
	installerInstruction = regexp.MustCompile(`(?i)\brun\s+(?:'([^']+)'|"([^"]+)"|(.+))`)
)

// installerStrings reads single-line quoted output arguments throughout the source.
func installerStrings(source []byte, powershell bool) []example {
	if !powershell {
		return shellStrings(source)
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
	for _, match := range powershellFunction.FindAllSubmatchIndex(code, -1) {
		name := strings.ToLower(string(code[match[2]:match[3]]))
		bodies[match[0]+bytes.IndexByte(code[match[0]:], '{')] = name
	}
	var messages []example
	// Rescan until no new wrapper appears, so declaration order doesn't matter.
	for known := 0; known != len(printers); {
		known, messages = len(printers), nil
		call, boundary, metadata, owners := "", true, false, []string{}
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
			operator := text == "&&" || text == "||" || text == "|"
			if strings.ContainsAny(text, ";{}\n") && len(text) == 1 || operator {
				call, boundary, metadata = "", true, false
				continue
			}
			if boundary {
				name := strings.ToLower(text)
				if printers[name] || strings.HasPrefix(name, "write-") {
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
				name := strings.ToLower(text)
				metadata = strings.HasPrefix(name, "-") && !strings.Contains(name, ":") && !slices.Contains([]string{"-object", "-message", "-messagedata", "-nonewline", "-noenumerate"}, name)
				if strings.Contains(text, "(") {
					call = ""
				}
				continue
			}
			value := colorPrefix.ReplaceAllString(text[1:len(text)-1], "")
			if metadata || text[0] == '"' && expands(value, true) {
				metadata = false
				continue
			}
			if text[0] == '\'' {
				value = strings.ReplaceAll(value, "''", "'")
			} else {
				value = strings.ReplaceAll(strings.NewReplacer("`n", "\n", "`t", "\t", "`e", "\x1b", "`\"", "\"", "``", "`", "`$", "$").Replace(value), "\"\"", "\"")
			}
			messages = append(messages, example{value, bytes.Count(source[:match[0]], []byte{'\n'}) + 1})
		}
	}
	return messages
}

// shellStrings reads output arguments from parsed calls, excluding redirects and heredocs.
func shellStrings(source []byte) []example {
	tree, err := syntax.NewParser().Parse(bytes.NewReader(source), "")
	if err != nil {
		return nil
	}
	type outputCall struct {
		call  *syntax.CallExpr
		owner string
	}
	var calls []outputCall
	var collect func(syntax.Node, string)
	collect = func(root syntax.Node, owner string) {
		syntax.Walk(root, func(node syntax.Node) bool {
			switch node := node.(type) {
			case *syntax.Redirect:
				return false
			case *syntax.FuncDecl:
				collect(node.Body, node.Name.Value)
				return false
			case *syntax.CallExpr:
				calls = append(calls, outputCall{node, owner})
				return false
			}
			return true
		})
	}
	collect(tree, "")
	// Index zero means every argument; wrapper entries name the parameters they forward.
	printers := map[string]map[int]bool{"echo": {0: true}, "printf": {0: true}}
	for changed := true; changed; {
		changed = false
		for _, entry := range calls {
			if len(entry.call.Args) == 0 || entry.owner == "" {
				continue
			}
			forwarded := printers[entry.call.Args[0].Lit()]
			for i, arg := range entry.call.Args[1:] {
				if !forwarded[0] && !forwarded[i+1] {
					continue
				}
				syntax.Walk(arg, func(node syntax.Node) bool {
					if _, ok := node.(*syntax.CmdSubst); ok {
						return false
					}
					if param, ok := node.(*syntax.ParamExp); ok {
						index, err := strconv.Atoi(param.Param.Value)
						if param.Param.Value == "@" || param.Param.Value == "*" {
							index, err = 0, nil
						}
						if err == nil && (index > 0 || param.Param.Value == "@" || param.Param.Value == "*") && !printers[entry.owner][index] {
							if printers[entry.owner] == nil {
								printers[entry.owner] = map[int]bool{}
							}
							printers[entry.owner][index], changed = true, true
						}
					}
					return true
				})
			}
		}
	}
	var messages []example
	for _, entry := range calls {
		if len(entry.call.Args) == 0 {
			continue
		}
		name := entry.call.Args[0].Lit()
		printed := printers[name]
		escapes, argument := false, 0
		for i, arg := range entry.call.Args[1:] {
			if !printed[0] && !printed[i+1] {
				continue
			}
			text := string(source[arg.Pos().Offset():arg.End().Offset()])
			if name == "printf" && text == "-v" {
				break
			}
			if name == "echo" && strings.HasPrefix(text, "-") {
				if option := strings.LastIndexAny(text, "eE"); option > 0 {
					escapes = text[option] == 'e'
				}
			}
			if len(text) < 2 || text[0] != '\'' && text[0] != '"' || text[len(text)-1] != text[0] {
				if !strings.HasPrefix(text, "-") {
					argument++
				}
				continue
			}
			value := colorPrefix.ReplaceAllString(text[1:len(text)-1], "")
			if arg.Pos().Line() != entry.call.Pos().Line() || strings.Contains(text, "\n") || text[0] == '"' && expands(value, false) {
				argument++
				continue
			}
			if text[0] == '"' {
				value = strings.NewReplacer(`\\`, `\`, `\"`, `"`, `\$`, `$`, "\\`", "`").Replace(value)
			}
			if name == "printf" && argument == 0 || escapes {
				value, _, _ = expand.Format(nil, strings.ReplaceAll(value, "%", "%%"), nil)
			}
			argument++
			messages = append(messages, example{value, int(arg.Pos().Line())})
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
				candidates := []string{value}
				if instruction := installerInstruction.FindStringSubmatch(value); instruction != nil {
					candidates = append(candidates, strings.Join(instruction[1:], ""))
				}
				for _, candidate := range candidates {
					found := false
					for more := true; more; _, candidate, more = strings.Cut(candidate, ":") {
						if add(candidate, line) {
							found = true
							break
						}
					}
					if found {
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
