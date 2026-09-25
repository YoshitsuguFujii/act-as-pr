package main

import (
	"bytes"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestModuleIsAvailableAtItsPublicRepositoryPath(t *testing.T) {
	cmd := exec.Command("go", "list", "-m")
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("read module identity: %v: %s", err, out)
	}
	if got := strings.TrimSpace(string(out)); got != "github.com/YoshitsuguFujii/act-as-pr" {
		t.Fatalf("public module path: got %q", got)
	}
}

func TestRunExplainsRequiredBase(t *testing.T) {
	var output bytes.Buffer
	err := run(nil, &output)
	if err == nil || !strings.Contains(err.Error(), "usage: act-as-pr <base>") {
		t.Fatalf("missing base should show usage, got %v", err)
	}
}

func TestInspectRendersAnEmptyComparison(t *testing.T) {
	dir := repo(t)
	put(t, dir, "readme.txt", "unchanged\n")
	commit(t, dir, "base")
	view, err := inspect(dir, "HEAD")
	if err != nil {
		t.Fatal(err)
	}
	if len(view.Files) != 0 || view.Additions != 0 || view.Deletions != 0 {
		t.Fatalf("expected an empty comparison: %+v", view)
	}
	page, err := render(view)
	if err != nil || !strings.Contains(string(page), "No changes") {
		t.Fatalf("expected empty state: %v", err)
	}
}

func gitTest(t *testing.T, dir string, args ...string) string {
	t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git %v: %v: %s", args, err, out)
	}
	return strings.TrimSpace(string(out))
}

func repo(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	gitTest(t, dir, "init", "-q", "-b", "main")
	gitTest(t, dir, "config", "user.email", "test@example.com")
	gitTest(t, dir, "config", "user.name", "Test")
	return dir
}

func put(t *testing.T, dir, name, content string) {
	t.Helper()
	path := filepath.Join(dir, name)
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0644); err != nil {
		t.Fatal(err)
	}
}

func commit(t *testing.T, dir, msg string) {
	t.Helper()
	gitTest(t, dir, "add", "-A")
	gitTest(t, dir, "commit", "-qm", msg)
}

func TestInspectUsesMergeBaseFromSubdirectory(t *testing.T) {
	dir := repo(t)
	put(t, dir, "nested/keep.txt", "base\n")
	commit(t, dir, "base")
	gitTest(t, dir, "checkout", "-qb", "feature")
	put(t, dir, "nested/keep.txt", "feature\n")
	commit(t, dir, "feature")
	gitTest(t, dir, "checkout", "main")
	put(t, dir, "main-only.txt", "main\n")
	commit(t, dir, "main advances")
	gitTest(t, dir, "checkout", "feature")

	view, err := inspect(filepath.Join(dir, "nested"), "main")
	if err != nil {
		t.Fatal(err)
	}
	if view.Repository != filepath.Base(dir) || view.Head != "feature" || len(view.Files) != 1 || view.Files[0].Path != "nested/keep.txt" {
		t.Fatalf("wrong PR comparison: %+v", view)
	}
	if view.Additions != 1 || view.Deletions != 1 {
		t.Fatalf("wrong totals: %+v", view)
	}
}

func TestRenderIdentifiesBaseAndCompareAndGroupsChangedPaths(t *testing.T) {
	dir := repo(t)
	put(t, dir, "README.md", "base\n")
	commit(t, dir, "base")
	gitTest(t, dir, "checkout", "-qb", "feature")
	put(t, dir, "src/api/one.go", "package api\n")
	put(t, dir, "src/api/two.go", "package api\n")
	put(t, dir, "docs/guide.md", "guide\n")
	commit(t, dir, "feature")

	view, err := inspect(dir, "main")
	if err != nil {
		t.Fatal(err)
	}
	page, err := render(view)
	if err != nil {
		t.Fatal(err)
	}
	html := string(page)
	base := strings.Index(html, "base: <strong>main</strong>")
	compare := strings.Index(html, "compare: <strong>feature</strong>")
	if base < 0 || compare < base {
		t.Fatal("comparison direction should read base: main, then compare: feature")
	}
	start, end := strings.Index(html, `<nav id="file-nav">`), strings.Index(html, "</nav>")
	if start < 0 || end < start {
		t.Fatal("changed-file navigation missing")
	}
	nav := html[start:end]
	if strings.Count(nav, `aria-label="src/api"`) != 1 || strings.Count(nav, `aria-label="docs"`) != 1 {
		t.Fatal("changed files should be grouped by directory")
	}
	for _, path := range []string{"src/api/one.go", "src/api/two.go", "docs/guide.md"} {
		if !strings.Contains(nav, `title="`+path+`"`) {
			t.Errorf("navigation missing %s", path)
		}
	}
}

func TestInspectShowsFileKindsAndUnusualPaths(t *testing.T) {
	dir := repo(t)
	put(t, dir, "edit me.txt", "before\n")
	put(t, dir, "delete.txt", "gone\n")
	put(t, dir, "rename me.txt", "same\n")
	commit(t, dir, "base")
	gitTest(t, dir, "checkout", "-qb", "feature")
	put(t, dir, "edit me.txt", "after\n")
	if err := os.Remove(filepath.Join(dir, "delete.txt")); err != nil {
		t.Fatal(err)
	}
	if err := os.Rename(filepath.Join(dir, "rename me.txt"), filepath.Join(dir, "renamed.txt")); err != nil {
		t.Fatal(err)
	}
	put(t, dir, "a <script>.txt", "added\n")
	put(t, dir, "line\nbreak.txt", "odd\n")
	put(t, dir, "binary.dat", "\x00\x01\x02")
	commit(t, dir, "changes")

	view, err := inspect(dir, "main")
	if err != nil {
		t.Fatal(err)
	}
	kinds := map[string]string{}
	for _, f := range view.Files {
		kinds[f.Path] = f.Status
	}
	for name, want := range map[string]string{"edit me.txt": "modified", "delete.txt": "deleted", "renamed.txt": "renamed", "a <script>.txt": "added", "line\nbreak.txt": "added", "binary.dat": "added"} {
		if kinds[name] != want {
			t.Errorf("%q: got %q, want %q", name, kinds[name], want)
		}
	}
	if len(view.Files) != 6 {
		t.Fatalf("got %d files", len(view.Files))
	}
	var binary bool
	for _, f := range view.Files {
		if f.Path == "binary.dat" {
			binary = f.Binary
		}
	}
	if !binary {
		t.Fatal("binary file should have a readable placeholder")
	}
}

func TestInspectNumbersLinesAcrossHunks(t *testing.T) {
	dir := repo(t)
	put(t, dir, "sample.go", "one\ntwo\nthree\nfour\nfive\nsix\nseven\neight\nnine\nten\n")
	commit(t, dir, "base")
	gitTest(t, dir, "checkout", "-qb", "feature")
	put(t, dir, "sample.go", "one\nTWO\nthree\nfour\nfive\nsix\nseven\neight\nnine\nTEN\n")
	commit(t, dir, "edits")
	view, err := inspect(dir, "main")
	if err != nil {
		t.Fatal(err)
	}
	var oldTwo, newTwo, oldTen, newTen int
	for _, h := range view.Files[0].Hunks {
		for _, l := range h.Lines {
			switch l.Text {
			case "two":
				oldTwo = l.Old
			case "TWO":
				newTwo = l.New
			case "ten":
				oldTen = l.Old
			case "TEN":
				newTen = l.New
			}
		}
	}
	if oldTwo != 2 || newTwo != 2 || oldTen != 10 || newTen != 10 {
		t.Fatalf("line numbers: %d %d %d %d", oldTwo, newTwo, oldTen, newTen)
	}
}

func TestRenderEscapesRepositoryContentAndIsSelfContained(t *testing.T) {
	dir := repo(t)
	put(t, dir, "safe.txt", "initial\n")
	commit(t, dir, "base")
	gitTest(t, dir, "checkout", "-qb", "feature")
	put(t, dir, "<script>alert(1)</script>.txt", "<script>alert(2)</script>\n")
	commit(t, dir, "hostile content")
	view, err := inspect(dir, "main")
	if err != nil {
		t.Fatal(err)
	}
	page, err := render(view)
	if err != nil {
		t.Fatal(err)
	}
	html := string(page)
	if strings.Contains(html, "<script>alert(1)</script>") || strings.Contains(html, "<script>alert(2)</script>") {
		t.Fatal("untrusted script appeared as markup")
	}
	if !strings.Contains(html, "&lt;script&gt;alert(2)&lt;/script&gt;") {
		t.Fatal("diff content missing or unescaped")
	}
	for _, required := range []string{"Split", "Unified", "file-nav", "file-toggle", "prefers-color-scheme", "Content-Security-Policy"} {
		if !strings.Contains(html, required) {
			t.Errorf("missing %s", required)
		}
	}
	if strings.Contains(html, "http://") || strings.Contains(html, "https://") {
		t.Fatal("viewer must not depend on network")
	}
}

func TestInspectRejectsInvalidInputs(t *testing.T) {
	if _, err := inspect(t.TempDir(), "main"); err == nil {
		t.Fatal("non-repository accepted")
	}
	dir := repo(t)
	put(t, dir, "a", "a")
	commit(t, dir, "base")
	if _, err := inspect(dir, "missing-revision"); err == nil {
		t.Fatal("missing base accepted")
	}
	if _, err := inspect(dir, "--output=/tmp/oops"); err == nil {
		t.Fatal("option-like base accepted")
	}
}

func TestInspectReportsGitFailure(t *testing.T) {
	dir := repo(t)
	put(t, dir, "a", "a")
	commit(t, dir, "base")
	t.Setenv("PATH", t.TempDir())
	if _, err := inspect(dir, "main"); err == nil {
		t.Fatal("missing git executable accepted")
	}
}

func TestWritePreviewCreatesHtmlOutsideRepository(t *testing.T) {
	dir := repo(t)
	put(t, dir, "a", "a")
	commit(t, dir, "base")
	view, err := inspect(dir, "HEAD")
	if err != nil {
		t.Fatal(err)
	}
	path, err := writePreview(view)
	if err != nil {
		t.Fatal(err)
	}
	if strings.HasPrefix(path, dir) {
		t.Fatalf("output inside repository: %s", path)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(data), "Files changed") {
		t.Fatal("invalid preview")
	}
}
