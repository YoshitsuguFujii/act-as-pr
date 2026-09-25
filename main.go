package main

import (
	"bytes"
	"embed"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path"
	"path/filepath"
	"regexp"
	"runtime"
	"strconv"
	"strings"
)

//go:embed viewer.html viewer.css viewer.js
var assets embed.FS

type Line struct {
	Kind, Text string
	Old, New   int
}
type Hunk struct {
	Header string
	Lines  []Line
}
type SplitRow struct {
	Header      string
	Left, Right *Line
}
type File struct {
	ID, Path, OldPath, Status   string
	Binary                      bool
	Staged, Unstaged, Untracked bool
	Additions, Deletions        int
	Hunks                       []Hunk
	Split                       []SplitRow
}
type NavFile struct {
	ID, Path, Name, Status      string
	Staged, Unstaged, Untracked bool
}
type NavGroup struct {
	Directory string
	Files     []NavFile
}
type View struct {
	ID                                      string
	Repository, Root, Base, Head, MergeBase string
	Files                                   []File
	NavGroups                               []NavGroup
	Additions, Deletions                    int
	patchBytes                              int
}

const maxGitOutput = 32 << 20

type limitBuffer struct {
	bytes.Buffer
	max int
}

func (b *limitBuffer) Write(p []byte) (int, error) {
	if b.Len()+len(p) > b.max {
		return 0, fmt.Errorf("diff exceeds %d MiB limit", b.max>>20)
	}
	return b.Buffer.Write(p)
}

func git(dir string, args ...string) ([]byte, error) {
	return gitWithAllowedExit(dir, false, args...)
}

func gitWithAllowedExit(dir string, allowDifference bool, args ...string) ([]byte, error) {
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	cmd.Env = append(os.Environ(), "GIT_OPTIONAL_LOCKS=0", "GIT_PAGER=cat")
	stdout := &limitBuffer{max: maxGitOutput}
	var stderr bytes.Buffer
	cmd.Stdout, cmd.Stderr = stdout, &stderr
	if err := cmd.Run(); err != nil {
		var exit *exec.ExitError
		if allowDifference && errors.As(err, &exit) && exit.ExitCode() == 1 && stderr.Len() == 0 {
			return stdout.Bytes(), nil
		}
		if stderr.Len() > 0 {
			return nil, fmt.Errorf("git %s: %w: %s", args[0], err, strings.TrimSpace(stderr.String()))
		}
		return nil, fmt.Errorf("git %s: %w", args[0], err)
	}
	return stdout.Bytes(), nil
}

func inspect(dir, base string) (View, error) {
	var v View
	if base == "" || strings.HasPrefix(base, "-") {
		return v, errors.New("base revision is required and must not start with '-'")
	}
	root, err := git(dir, "rev-parse", "--show-toplevel")
	if err != nil {
		return v, fmt.Errorf("not a Git repository: %w", err)
	}
	v.Root = strings.TrimSpace(string(root))
	v.Repository = filepath.Base(v.Root)
	resolved, err := git(v.Root, "rev-parse", "--verify", "--end-of-options", base+"^{commit}")
	if err != nil {
		return v, fmt.Errorf("invalid base revision %q: %w", base, err)
	}
	v.Base = base
	merge, err := git(v.Root, "merge-base", strings.TrimSpace(string(resolved)), "HEAD")
	if err != nil {
		return v, fmt.Errorf("no merge base with %q: %w", base, err)
	}
	v.MergeBase = strings.TrimSpace(string(merge))
	head, err := git(v.Root, "symbolic-ref", "--quiet", "--short", "HEAD")
	if err != nil {
		head, err = git(v.Root, "rev-parse", "--short", "HEAD")
		if err != nil {
			return v, err
		}
	}
	v.Head = strings.TrimSpace(string(head))
	diff, _, err := inspectDiff(v.Root, v.MergeBase, "HEAD", "file")
	if err != nil {
		return v, err
	}
	v.ID, v.Files, v.NavGroups, v.Additions, v.Deletions, v.patchBytes = diff.ID, diff.Files, diff.NavGroups, diff.Additions, diff.Deletions, diff.patchBytes
	return v, nil
}

func groupNavigation(files []File) []NavGroup {
	var groups []NavGroup
	positions := make(map[string]int)
	for _, file := range files {
		directory := path.Dir(file.Path)
		if directory == "." {
			directory = ""
		}
		index, found := positions[directory]
		if !found {
			index = len(groups)
			positions[directory] = index
			groups = append(groups, NavGroup{Directory: directory})
		}
		groups[index].Files = append(groups[index].Files, NavFile{ID: file.ID, Path: file.Path, Name: path.Base(file.Path), Status: file.Status, Staged: file.Staged, Unstaged: file.Unstaged, Untracked: file.Untracked})
	}
	return groups
}

func parseNames(raw []byte) ([]File, error) {
	if len(raw) == 0 {
		return nil, nil
	}
	if raw[len(raw)-1] != 0 {
		return nil, errors.New("malformed git file list")
	}
	fields := bytes.Split(raw[:len(raw)-1], []byte{0})
	var files []File
	for i := 0; i < len(fields); {
		status := string(fields[i])
		i++
		if i >= len(fields) || status == "" {
			return nil, errors.New("malformed git file list")
		}
		f := File{Path: string(fields[i])}
		i++
		switch status[0] {
		case 'A':
			f.Status = "added"
		case 'D':
			f.Status = "deleted"
		case 'M', 'T':
			f.Status = "modified"
		case 'R', 'C':
			if i >= len(fields) {
				return nil, errors.New("malformed rename")
			}
			f.OldPath = f.Path
			f.Path = string(fields[i])
			i++
			if status[0] == 'R' {
				f.Status = "renamed"
			} else {
				f.Status = "copied"
			}
		default:
			return nil, fmt.Errorf("unsupported git file status %q", status)
		}
		files = append(files, f)
	}
	return files, nil
}

func splitPatch(patch string) []string {
	if patch == "" {
		return nil
	}
	var sections []string
	var current strings.Builder
	for _, line := range strings.SplitAfter(patch, "\n") {
		if strings.HasPrefix(line, "diff --git ") {
			if current.Len() > 0 {
				sections = append(sections, current.String())
				current.Reset()
			}
		}
		if current.Len() > 0 || strings.HasPrefix(line, "diff --git ") {
			current.WriteString(line)
		}
	}
	if current.Len() > 0 {
		sections = append(sections, current.String())
	}
	return sections
}

var hunkPattern = regexp.MustCompile(`^@@ -(\d+)(?:,\d+)? \+(\d+)(?:,\d+)? @@`)

func parsePatch(f *File, patch string) {
	var h *Hunk
	old, next := 0, 0
	for _, raw := range strings.Split(patch, "\n") {
		if strings.HasPrefix(raw, "Binary files ") || strings.HasPrefix(raw, "GIT binary patch") {
			f.Binary = true
		}
		if match := hunkPattern.FindStringSubmatch(raw); match != nil {
			old, _ = strconv.Atoi(match[1])
			next, _ = strconv.Atoi(match[2])
			f.Hunks = append(f.Hunks, Hunk{Header: raw})
			h = &f.Hunks[len(f.Hunks)-1]
			continue
		}
		if h == nil || raw == "" {
			continue
		}
		l := Line{Text: raw[1:]}
		switch raw[0] {
		case ' ':
			l.Kind = "context"
			l.Old = old
			l.New = next
			old++
			next++
		case '-':
			l.Kind = "delete"
			l.Old = old
			old++
			f.Deletions++
		case '+':
			l.Kind = "add"
			l.New = next
			next++
			f.Additions++
		default:
			continue
		}
		h.Lines = append(h.Lines, l)
	}
}

func splitRows(hunks []Hunk) []SplitRow {
	var rows []SplitRow
	for _, h := range hunks {
		rows = append(rows, SplitRow{Header: h.Header})
		for i := 0; i < len(h.Lines); {
			if h.Lines[i].Kind == "context" {
				l := h.Lines[i]
				rows = append(rows, SplitRow{Left: &l, Right: &l})
				i++
				continue
			}
			var deleted, added []*Line
			for i < len(h.Lines) && h.Lines[i].Kind != "context" {
				l := h.Lines[i]
				if l.Kind == "delete" {
					deleted = append(deleted, &l)
				} else {
					added = append(added, &l)
				}
				i++
			}
			n := len(deleted)
			if len(added) > n {
				n = len(added)
			}
			for j := 0; j < n; j++ {
				var row SplitRow
				if j < len(deleted) {
					row.Left = deleted[j]
				}
				if j < len(added) {
					row.Right = added[j]
				}
				rows = append(rows, row)
			}
		}
	}
	return rows
}

func writePreview(v View) (string, error) {
	page, err := render(v)
	if err != nil {
		return "", err
	}
	return writeHTML(page)
}

func writeAppPreview(app App) (string, error) {
	page, err := renderApp(app)
	if err != nil {
		return "", err
	}
	return writeHTML(page)
}

func writeHTML(page []byte) (string, error) {
	dir := filepath.Join(os.TempDir(), "act-as-pr")
	if err := os.MkdirAll(dir, 0700); err != nil {
		return "", err
	}
	file, err := os.CreateTemp(dir, "preview-*.html")
	if err != nil {
		return "", err
	}
	name := file.Name()
	if err := file.Chmod(0600); err != nil {
		file.Close()
		os.Remove(name)
		return "", err
	}
	if _, err := file.Write(page); err != nil {
		file.Close()
		os.Remove(name)
		return "", err
	}
	if err := file.Close(); err != nil {
		os.Remove(name)
		return "", err
	}
	return name, nil
}

func openBrowser(path string) error {
	var cmd *exec.Cmd
	switch runtime.GOOS {
	case "darwin":
		cmd = exec.Command("open", path)
	case "linux":
		cmd = exec.Command("xdg-open", path)
	default:
		return fmt.Errorf("opening the browser is unsupported on %s", runtime.GOOS)
	}
	if out, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("browser opener: %w: %s", err, strings.TrimSpace(string(out)))
	}
	return nil
}

func run(args []string, out io.Writer) error {
	base, watch, err := parseCLI(args)
	if err != nil {
		return err
	}
	cwd, err := os.Getwd()
	if err != nil {
		return err
	}
	if watch {
		return runWatch(cwd, base, out)
	}
	app, err := inspectApp(cwd, base)
	if err != nil {
		return err
	}
	path, err := writeAppPreview(app)
	if err != nil {
		return err
	}
	if err := openBrowser(path); err != nil {
		return fmt.Errorf("preview saved at %s; %w", path, err)
	}
	fmt.Fprintf(out, "Opened %s (%d changed files)\n", path, len(app.FilesChanged.Files))
	return nil
}

func parseCLI(args []string) (string, bool, error) {
	usage := errors.New("usage: act-as-pr <base> [--watch] (example: act-as-pr main --watch)")
	var base string
	var watch bool
	for _, arg := range args {
		switch {
		case arg == "--watch" && !watch:
			watch = true
		case arg == "" || strings.HasPrefix(arg, "-") || base != "":
			return "", false, usage
		default:
			base = arg
		}
	}
	if base == "" {
		return "", false, usage
	}
	return base, watch, nil
}

func main() {
	if err := run(os.Args[1:], os.Stdout); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
