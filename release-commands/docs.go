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
	markdownExtensions  = []string{".md", ".mdx", ".markdown"}
	powershellCalls     = regexp.MustCompile(`(?im)(?:^|[;{}|(&])[ \t]*([\w-]+)[ \t]+`)
	powershellFunctions = regexp.MustCompile(`(?i)\bfunction\s+([\w-]+)`)
	powershellHelpers   = regexp.MustCompile(`(?im)^[ \t]*function\s+([\w-]+)\((?:\[string\])?(\$\w+)\)[ \t]*\{[ \t]*(write-[\w-]+)[ \t]+(\$\w+)(?:[ \t][^{}\r\n]*)?\}`)
	powershellArguments = regexp.MustCompile(`^[ \t]*('(?:''|[^'\r\n])*'|"[^"\r\n]*")`)
	shellEscape         = regexp.MustCompile(`\\(?:0[0-7]{0,3}|[1-7][0-7]{0,2}|.)`)
	printfConversion    = regexp.MustCompile(`\\.|%[-+ 0-9]*[[:alpha:]%]`)
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
		if text[0] == '\'' {
			value = strings.ReplaceAll(value, "''", "'")
		}
		if text[0] == '\'' || !strings.ContainsAny(value, "$`") {
			messages = append(messages, example{text: value, line: bytes.Count(source[:match[3]], []byte{'\n'}) + 1})
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
		case quote == 0 && c == '@' && i+1 < len(masked) && (masked[i+1] == '\'' || masked[i+1] == '"'):
			end := bytes.Index(masked[i:], []byte{'\n', masked[i+1], '@'})
			if end < 0 {
				end = len(masked) - i
			}
			for j := i; j < i+end; j++ {
				if masked[j] != '\n' {
					masked[j] = ' '
				}
			}
			i += end + 2
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
			switch node.(type) {
			case *syntax.FuncDecl, *syntax.Redirect:
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
		if _, ok := node.(*syntax.Redirect); ok {
			return false
		}
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
	if call.Pos().Line() != call.End().Line() {
		return nil, false
	}
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
	args := words[1:]
	switch words[0] {
	case "echo":
		escapes := false
		for len(args) > 0 && strings.HasPrefix(args[0], "-") && len(args[0]) > 1 && strings.Trim(args[0][1:], "neE") == "" {
			for _, option := range args[0][1:] {
				switch option {
				case 'e':
					escapes = true
				case 'E':
					escapes = false
				}
			}
			args = args[1:]
		}
		output := strings.Join(args, " ")
		if escapes {
			output, _ = shellEscapes(output, true)
		}
		return output, true
	case "printf":
		format, args := args[0], args[1:]
		if format == "--" && len(args) > 0 {
			format, args = args[0], args[1:]
		} else if format != "-" && strings.HasPrefix(format, "-") {
			return "", false
		}
		var output strings.Builder
		for {
			rendering, values, index, stopped := format, slices.Clone(args), 0, false
			for _, span := range printfConversion.FindAllStringIndex(format, -1) {
				conversion := format[span[0]:span[1]]
				if conversion[0] != '%' || conversion[len(conversion)-1] == '%' {
					continue
				}
				if conversion[len(conversion)-1] == 'b' && index < len(values) {
					values[index], stopped = shellEscapes(values[index], false)
					rendering = rendering[:span[1]-1] + "s" + rendering[span[1]:]
					if stopped {
						rendering, values = rendering[:span[1]], values[:index+1]
						break
					}
				}
				index++
			}
			rendered, used, err := expand.Format(nil, rendering, values)
			output.WriteString(rendered)
			if err != nil || stopped || used == 0 || used == len(args) {
				return output.String(), err == nil
			}
			args = args[used:]
		}
	}
	return "", false
}

// echo -e requires a leading zero for octal; both printers stop at an unescaped \c.
func shellEscapes(value string, echo bool) (string, bool) {
	stopped := false
	for _, span := range slices.Backward(shellEscape.FindAllStringIndex(value, -1)) {
		escape := value[span[0]:span[1]]
		switch {
		case escape == `\c`:
			value, stopped = value[:span[0]], true
		case echo && escape[1] >= '1' && escape[1] <= '7':
			value = value[:span[0]] + `\` + value[span[0]:]
		case escape[1] >= '0' && escape[1] <= '7':
			n := 0
			for _, digit := range strings.TrimPrefix(escape[1:], "0") {
				n = n*8 + int(digit-'0')
			}
			value = value[:span[0]] + fmt.Sprintf(`\%03o`, byte(n)) + value[span[1]:]
		}
	}
	rendered, _, _ := expand.Format(nil, value, nil)
	return rendered, stopped
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
							if !prompted && index+1 < len(segments) {
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
