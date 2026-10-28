// Package siblingpins checks that every revision a repository pins from a sibling
// kenn-io repository is on that repository's main branch. A pull request may
// pin a sibling's pull request build while it is open, but main may not: a
// squash merge never puts that commit on the sibling's main, and Go cannot
// download a commit reachable only through a pull request ref.
// Go programs call Collect and then GitHub.Check.
package siblingpins

import (
	"errors"
	"fmt"
	"io/fs"
	"path"
	"regexp"
	"slices"
	"strings"

	"golang.org/x/mod/modfile"
	"golang.org/x/mod/module"
	"sigs.k8s.io/yaml"
)

// Owner is the GitHub organization whose repositories count as siblings.
const Owner = "kenn-io"

// Sibling Go modules use the vanity host, where go.kenn.io/NAME/... is
// served from github.com/kenn-io/NAME, or their GitHub path directly.
var goPrefixes = []string{"go.kenn.io/", "github.com/" + Owner + "/"}

// siblingRepo returns the sibling repository and the module's directory in
// it, or ok false when modulePath is not a sibling module.
func siblingRepo(modulePath string) (repo, dir string, ok bool) {
	for _, prefix := range goPrefixes {
		if rest, found := strings.CutPrefix(modulePath, prefix); found {
			repo, dir, _ = strings.Cut(rest, "/")
			return repo, dir, true
		}
	}
	return "", "", false
}

func isSiblingModule(modulePath string) bool {
	_, _, ok := siblingRepo(modulePath)
	return ok
}

// Pin is one sibling revision named by a repository file.
type Pin struct {
	File string // path relative to the scanned root
	Repo string // repository name under Owner
	Ref  string // commit (full or abbreviated) or tag
	// Source names the file and dependency for messages, such as
	// "go.mod: go.kenn.io/forge v0.4.1-0.20261020014253-5fc3781f38fc".
	Source string
}

var (
	// Bun and npm spell GitHub dependencies several ways:
	//   git+https://github.com/kenn-io/app-ui.git/#<ref>
	//   git+ssh://git@github.com/kenn-io/app-ui.git#<ref>
	//   github:kenn-io/kit-ui#<ref>
	gitPin = regexp.MustCompile(`(?:github\.com[/:]|github:)` + Owner + `/([A-Za-z0-9._-]+?)(?:\.git)?/?#([^"'\s,]+)`)
	// go run and go install name a tool as PACKAGE@VERSION.
	goRunPin = regexp.MustCompile(`go\.kenn\.io/([A-Za-z0-9._-]+)[A-Za-z0-9._/-]*@([^\s"'` + "`" + `]+)`)
	// A step or job that uses a kenn-io action or reusable workflow. Only a
	// uses: key at the start of a line counts, not the same text in a script.
	actionUse = regexp.MustCompile(`(?m)^[ \t]*(?:-[ \t]+)?uses:[ \t]*["']?` + Owner + `/([A-Za-z0-9._-]+)(?:/[A-Za-z0-9._/-]*)?@([^\s"'#]+)`)
	// A simple Makefile assignment such as HUMA_CHECK_VERSION := v1.2.3.
	makeAssignment = regexp.MustCompile(`(?m)^[ \t]*(?:override[ \t]+|export[ \t]+)?([A-Za-z0-9_]+)[ \t]*(?::{1,3}|\?|\+)?=[ \t]*(\S+)[ \t]*$`)
	makeReference  = regexp.MustCompile(`\$[({]([A-Za-z0-9_]+)[)}]`)
	safeRef        = regexp.MustCompile(`^[A-Za-z0-9._/-]+$`)
	safeRepo       = regexp.MustCompile(`^[A-Za-z0-9._-]+$`)
)

// Collect walks fsys and returns every sibling pin named by go.mod,
// package.json, bun.lock, Makefile, and .custom-gcl.yml files. Pins that
// cannot be checked, such as a sibling module replaced by a fork, are
// returned as errors.
func Collect(fsys fs.FS) ([]Pin, error) {
	var pins []Pin
	var problems []error
	err := fs.WalkDir(fsys, ".", func(name string, entry fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if entry.IsDir() && (entry.Name() == "node_modules" || entry.Name() == ".git") {
			return fs.SkipDir
		}
		parse := parserFor(name)
		if parse == nil || !entry.Type().IsRegular() {
			return nil
		}
		data, err := fs.ReadFile(fsys, name)
		if err != nil {
			return err
		}
		found, problem := parse(name, data)
		for i := range found {
			found[i].File = name
		}
		pins = append(pins, found...)
		if problem != nil {
			problems = append(problems, problem)
		}
		return nil
	})
	if err != nil {
		return nil, fmt.Errorf("scan repository files: %w", err)
	}
	return pins, errors.Join(problems...)
}

var parsers = map[string]func(name string, data []byte) ([]Pin, error){
	"go.mod":          goModPins,
	"package.json":    gitPins,
	"bun.lock":        gitPins,
	"Makefile":        goRunPins,
	".custom-gcl.yml": golangciPins,
	"action.yml":      actionPins,
	"action.yaml":     actionPins,
}

// parserFor returns the parser for a file, or nil. Workflows live under
// .github/workflows with any name.
func parserFor(name string) func(name string, data []byte) ([]Pin, error) {
	if path.Dir(name) == ".github/workflows" && (path.Ext(name) == ".yml" || path.Ext(name) == ".yaml") {
		return actionPins
	}
	return parsers[path.Base(name)]
}

// actionPins finds kenn-io actions a workflow or composite action uses, such
// as kenn-io/actions/sibling-pins@<commit>.
func actionPins(name string, data []byte) ([]Pin, error) {
	var pins pinList
	for _, match := range actionUse.FindAllSubmatch(data, -1) {
		pins.add(newPin(string(match[1]), string(match[2]), fmt.Sprintf("%s: %s", name, match[0])))
	}
	return pins.pins, errors.Join(pins.problems...)
}

func goModPins(name string, data []byte) ([]Pin, error) {
	file, err := modfile.Parse(name, data, nil)
	if err != nil {
		return nil, fmt.Errorf("parse %s: %w", name, err)
	}
	// A replacement without an old version covers every version of the path;
	// one with a version covers only that version.
	replaced := make(map[module.Version]bool, len(file.Replace))
	var pins pinList
	for _, replace := range file.Replace {
		replaced[replace.Old] = true
		switch {
		case replace.New.Version == "":
			// A directory replacement builds from this checkout, which this
			// check already scans.
		case isSiblingModule(replace.New.Path):
			pins.add(goModulePin(name, replace.New))
		case isSiblingModule(replace.Old.Path):
			pins.problems = append(pins.problems, fmt.Errorf(
				"%s: replace %s => %s %s: a sibling module must come from its own repository, not a fork",
				name, replace.Old.Path, replace.New.Path, replace.New.Version))
		}
	}
	for _, require := range file.Require {
		if replaced[require.Mod] || replaced[module.Version{Path: require.Mod.Path}] ||
			!isSiblingModule(require.Mod.Path) {
			continue
		}
		pins.add(goModulePin(name, require.Mod))
	}
	return pins.pins, errors.Join(pins.problems...)
}

// goModulePin maps a sibling module version to the GitHub revision it
// names. A pseudo-version names a commit; any other version names a tag,
// prefixed by the module's directory when the module is not at the
// repository root.
func goModulePin(name string, version module.Version) (Pin, error) {
	source := fmt.Sprintf("%s: %s %s", name, version.Path, version.Version)
	prefix, _, ok := module.SplitPathVersion(version.Path)
	if !ok {
		return Pin{}, fmt.Errorf("%s: invalid module path", source)
	}
	repo, dir, _ := siblingRepo(prefix)
	ref, err := goRef(version.Version)
	if err != nil {
		return Pin{}, fmt.Errorf("%s: %w", source, err)
	}
	if dir != "" && !module.IsPseudoVersion(version.Version) {
		ref = path.Join(dir, ref)
	}
	return newPin(repo, ref, source)
}

// goRef returns the commit a pseudo-version names, or version itself, which
// is then a tag or a commit hash.
func goRef(version string) (string, error) {
	if module.IsPseudoVersion(version) {
		return module.PseudoVersionRev(version)
	}
	return strings.TrimSuffix(version, "+incompatible"), nil
}

// goRunPins finds go run and go install pins. The version is resolved against
// the repository root, which is where every kenn-io module lives. A version
// may name a variable that the same Makefile assigns once.
func goRunPins(name string, data []byte) ([]Pin, error) {
	variables := map[string]string{}
	for _, match := range makeAssignment.FindAllSubmatch(data, -1) {
		variables[string(match[1])] = string(match[2])
	}
	var pins pinList
	for _, match := range goRunPin.FindAllSubmatch(data, -1) {
		source := fmt.Sprintf("%s: %s", name, match[0])
		version := makeReference.ReplaceAllStringFunc(string(match[2]), func(reference string) string {
			if value, ok := variables[makeReference.FindStringSubmatch(reference)[1]]; ok {
				return value
			}
			return reference
		})
		ref, err := goRef(version)
		if err != nil {
			pins.add(Pin{}, fmt.Errorf("%s: %w", source, err))
			continue
		}
		pins.add(newPin(string(match[1]), ref, source))
	}
	return pins.pins, errors.Join(pins.problems...)
}

// golangciPins reads the module plugins golangci-lint builds into custom-gcl.
func golangciPins(name string, data []byte) ([]Pin, error) {
	var config struct {
		Plugins []struct {
			Module  string `json:"module"`
			Version string `json:"version"`
			Path    string `json:"path"`
		} `json:"plugins"`
	}
	if err := yaml.Unmarshal(data, &config); err != nil {
		return nil, fmt.Errorf("parse %s: %w", name, err)
	}
	var pins pinList
	for _, plugin := range config.Plugins {
		// A plugin with a path builds from this checkout.
		if plugin.Path == "" && isSiblingModule(plugin.Module) {
			pins.add(goModulePin(name, module.Version{Path: plugin.Module, Version: plugin.Version}))
		}
	}
	return pins.pins, errors.Join(pins.problems...)
}

func gitPins(name string, data []byte) ([]Pin, error) {
	var pins pinList
	for _, match := range gitPin.FindAllSubmatch(data, -1) {
		pins.add(newPin(string(match[1]), string(match[2]), fmt.Sprintf("%s: %s", name, match[0])))
	}
	return pins.pins, errors.Join(pins.problems...)
}

// newPin validates repo and ref before they go into a GitHub API path.
// Neither may contain URL syntax, and ref may not contain ".." because the
// check builds a "main...REF" comparison from it.
func newPin(repo, ref, source string) (Pin, error) {
	if !safeRepo.MatchString(repo) {
		return Pin{}, fmt.Errorf("%s: unsupported repository name %q", source, repo)
	}
	if !safeRef.MatchString(ref) || strings.Contains(ref, "..") {
		return Pin{}, fmt.Errorf("%s: unsupported revision %q", source, ref)
	}
	return Pin{Repo: repo, Ref: ref, Source: source}, nil
}

// pinList gathers one file's pins and problems. package.json and bun.lock
// repeat each dependency, so it keeps one copy of each.
type pinList struct {
	pins     []Pin
	problems []error
}

func (l *pinList) add(pin Pin, err error) {
	switch {
	case err != nil:
		l.problems = append(l.problems, err)
	case !slices.Contains(l.pins, pin):
		l.pins = append(l.pins, pin)
	}
}
