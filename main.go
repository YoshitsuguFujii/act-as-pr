package main

import (
	"bytes"
	"crypto/sha256"
	"embed"
	"encoding/base64"
	"errors"
	"flag"
	"fmt"
	"html/template"
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
	ID, Path, OldPath, Status string
	Binary                    bool
	Additions, Deletions      int
	Hunks                     []Hunk
	Split                     []SplitRow
}
type NavFile struct {
	ID, Path, Name, Status string
}
type NavGroup struct {
	Directory string
	Files     []NavFile
}
type View struct {
	Repository, Root, Base, Head, MergeBase string
	Files                                   []File
	NavGroups                               []NavGroup
	Additions, Deletions                    int
	CSS                                     template.CSS
	JS                                      template.JS
	CSP                                     string
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
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	cmd.Env = append(os.Environ(), "GIT_OPTIONAL_LOCKS=0", "GIT_PAGER=cat")
	stdout := &limitBuffer{max: maxGitOutput}
	var stderr bytes.Buffer
	cmd.Stdout, cmd.Stderr = stdout, &stderr
	if err := cmd.Run(); err != nil {
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
	names, err := git(v.Root, "diff", "--no-ext-diff", "--no-textconv", "--find-renames", "--name-status", "-z", v.MergeBase, "HEAD", "--")
	if err != nil {
		return v, err
	}
	v.Files, err = parseNames(names)
	if err != nil {
		return v, err
	}
	patch, err := git(v.Root, "diff", "--no-ext-diff", "--no-textconv", "--no-color", "--find-renames", "--patch", "--src-prefix=a/", "--dst-prefix=b/", v.MergeBase, "HEAD", "--")
	if err != nil {
		return v, err
	}
	sections := splitPatch(string(patch))
	if len(sections) != len(v.Files) {
		return v, fmt.Errorf("diff metadata/patch mismatch: %d files, %d patches", len(v.Files), len(sections))
	}
	for i := range v.Files {
		v.Files[i].ID = fmt.Sprintf("file-%d", i+1)
		parsePatch(&v.Files[i], sections[i])
		v.Files[i].Split = splitRows(v.Files[i].Hunks)
		v.Additions += v.Files[i].Additions
		v.Deletions += v.Files[i].Deletions
	}
	v.NavGroups = groupNavigation(v.Files)
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
		groups[index].Files = append(groups[index].Files, NavFile{ID: file.ID, Path: file.Path, Name: path.Base(file.Path), Status: file.Status})
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

func render(v View) ([]byte, error) {
	htmlBytes, err := assets.ReadFile("viewer.html")
	if err != nil {
		return nil, err
	}
	css, err := assets.ReadFile("viewer.css")
	if err != nil {
		return nil, err
	}
	js, err := assets.ReadFile("viewer.js")
	if err != nil {
		return nil, err
	}
	cssHash := sha256.Sum256(css)
	jsHash := sha256.Sum256(js)
	v.CSP = fmt.Sprintf("default-src 'none'; style-src 'sha256-%s'; script-src 'sha256-%s'; img-src data:; base-uri 'none'; form-action 'none'", base64.StdEncoding.EncodeToString(cssHash[:]), base64.StdEncoding.EncodeToString(jsHash[:]))
	v.CSS = template.CSS(css)
	v.JS = template.JS(js)
	tmpl, err := template.New("viewer").Parse(string(htmlBytes))
	if err != nil {
		return nil, err
	}
	var out bytes.Buffer
	if err := tmpl.Execute(&out, v); err != nil {
		return nil, err
	}
	return out.Bytes(), nil
}

func writePreview(v View) (string, error) {
	page, err := render(v)
	if err != nil {
		return "", err
	}
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
	fs := flag.NewFlagSet("act-as-pr", flag.ContinueOnError)
	fs.SetOutput(out)
	if err := fs.Parse(args); err != nil {
		return err
	}
	if fs.NArg() != 1 {
		return errors.New("usage: act-as-pr <base> (example: act-as-pr main)")
	}
	cwd, err := os.Getwd()
	if err != nil {
		return err
	}
	v, err := inspect(cwd, fs.Arg(0))
	if err != nil {
		return err
	}
	path, err := writePreview(v)
	if err != nil {
		return err
	}
	if err := openBrowser(path); err != nil {
		return fmt.Errorf("preview saved at %s; %w", path, err)
	}
	fmt.Fprintf(out, "Opened %s (%d changed files)\n", path, len(v.Files))
	return nil
}

func main() {
	if err := run(os.Args[1:], os.Stdout); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
