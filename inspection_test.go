package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func fileByPath(files []File, name string) (File, bool) {
	for _, file := range files {
		if file.Path == name {
			return file, true
		}
	}
	return File{}, false
}

func TestPRViewsKeepCommittedChangesAndIndividualCommitsSeparate(t *testing.T) {
	dir := repo(t)
	put(t, dir, "base.txt", "base\n")
	commit(t, dir, "base")
	gitTest(t, dir, "checkout", "-qb", "feature")
	put(t, dir, "first.txt", "first\n")
	commit(t, dir, "first change")
	put(t, dir, "second.txt", "second\n")
	commit(t, dir, "second change")
	put(t, dir, "third.txt", "third\n")
	commit(t, dir, "third change")
	put(t, dir, "working.txt", "not committed\n")

	app, err := inspectApp(dir, "main")
	if err != nil {
		t.Fatal(err)
	}
	if len(app.Commits) != 3 || app.Commits[0].Subject != "first change" || app.Commits[1].Subject != "second change" || app.Commits[2].Subject != "third change" {
		t.Fatalf("wrong PR commit range: %+v", app.Commits)
	}
	for i, name := range []string{"first.txt", "second.txt", "third.txt"} {
		if _, ok := fileByPath(app.FilesChanged.Files, name); !ok {
			t.Errorf("Files changed misses %s", name)
		}
		if len(app.Commits[i].Diff.Files) != 1 || app.Commits[i].Diff.Files[0].Path != name {
			t.Errorf("commit %d should contain only %s", i, name)
		}
		if app.Commits[i].SHA == "" || app.Commits[i].Author != "Test" || app.Commits[i].Timestamp == "" {
			t.Errorf("commit metadata missing: %+v", app.Commits[i])
		}
	}
	if len(app.FilesChanged.Files) != 3 {
		t.Fatalf("uncommitted files leaked into Files changed: %+v", app.FilesChanged.Files)
	}
	if app.Uncommitted == nil || len(app.Uncommitted.Files) != 1 || app.Uncommitted.Files[0].Path != "working.txt" {
		t.Fatalf("uncommitted pseudo-commit incorrect: %+v", app.Uncommitted)
	}
}

func TestWorkingViewsIncludeStagedUnstagedAndUntrackedFiles(t *testing.T) {
	dir := repo(t)
	put(t, dir, "tracked.txt", "base\n")
	put(t, dir, "unstaged.txt", "base\n")
	put(t, dir, "old name.txt", "rename content\n")
	put(t, dir, "delete.txt", "delete content\n")
	put(t, dir, ".gitignore", "ignored.tmp\n")
	commit(t, dir, "base")
	gitTest(t, dir, "checkout", "-qb", "feature")
	put(t, dir, "committed.txt", "committed\n")
	commit(t, dir, "feature")
	put(t, dir, "tracked.txt", "staged\n")
	gitTest(t, dir, "add", "tracked.txt")
	put(t, dir, "tracked.txt", "working tree\n")
	put(t, dir, "unstaged.txt", "unstaged\n")
	gitTest(t, dir, "mv", "old name.txt", "new name.txt")
	if err := os.Remove(filepath.Join(dir, "delete.txt")); err != nil {
		t.Fatal(err)
	}
	put(t, dir, "new <script>.txt", "<script>alert(1)</script>\n")
	put(t, dir, "odd\nname.txt", "odd\n")
	put(t, dir, "binary.dat", "\x00\x01\x02")
	put(t, dir, "ignored.tmp", "ignored\n")

	app, err := inspectApp(dir, "main")
	if err != nil {
		t.Fatal(err)
	}
	if len(app.FilesChanged.Files) != 1 || app.FilesChanged.Files[0].Path != "committed.txt" {
		t.Fatalf("Files changed must be committed-only: %+v", app.FilesChanged.Files)
	}
	if app.Uncommitted == nil {
		t.Fatal("uncommitted pseudo-commit missing")
	}
	for _, view := range []View{*app.Uncommitted, app.AllChanges} {
		for name, status := range map[string]string{"tracked.txt": "modified", "unstaged.txt": "modified", "new name.txt": "renamed", "delete.txt": "deleted", "new <script>.txt": "added", "odd\nname.txt": "added", "binary.dat": "added"} {
			file, ok := fileByPath(view.Files, name)
			if !ok || file.Status != status {
				t.Errorf("%s: expected %s in working view, got %+v", name, status, file)
			}
		}
		if _, ok := fileByPath(view.Files, "ignored.tmp"); ok {
			t.Fatal("ignored file should not appear")
		}
		binary, _ := fileByPath(view.Files, "binary.dat")
		if !binary.Binary {
			t.Fatal("untracked binary file should have a placeholder")
		}
	}
	if _, ok := fileByPath(app.AllChanges.Files, "committed.txt"); !ok {
		t.Fatal("All changes misses committed file")
	}
	if app.StagedCount == 0 || app.UnstagedCount == 0 || app.UntrackedCount != 3 {
		t.Fatalf("working change summary incorrect: staged=%d unstaged=%d untracked=%d", app.StagedCount, app.UnstagedCount, app.UntrackedCount)
	}
	page, err := renderApp(app)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(page), "<script>alert(1)</script>") || !strings.Contains(string(page), "&lt;script&gt;alert(1)&lt;/script&gt;") {
		t.Fatal("untracked diff content is not safely escaped")
	}
	allHTML := string(page)
	allHTML = allHTML[strings.Index(allHTML, `data-view="all"`):]
	for name, want := range map[string][]string{
		"tracked.txt":  {"Staged", "Unstaged"},
		"unstaged.txt": {"Unstaged"},
		"new name.txt": {"Staged"},
		"binary.dat":   {"Untracked"},
	} {
		start := strings.Index(allHTML, `<span class="file-path">`+name+`</span>`)
		if start < 0 {
			t.Fatalf("file header missing for %s", name)
		}
		end := strings.Index(allHTML[start:], "</summary>")
		if end < 0 {
			t.Fatalf("file header incomplete for %s", name)
		}
		header := allHTML[start : start+end]
		for _, label := range want {
			if !strings.Contains(header, `class="working-state`) || !strings.Contains(header, ">"+label+"</span>") {
				t.Errorf("%s should show %s in its file header: %s", name, label, header)
			}
		}
	}
	navStart := strings.Index(allHTML, `id="all-nav"`)
	if navStart < 0 {
		t.Fatal("All changes file navigation missing")
	}
	allNav := allHTML[navStart:]
	allNav = allNav[:strings.Index(allNav, "</nav>")]
	for name, want := range map[string][]string{
		"tracked.txt":  {`title="Staged">S</span>`, `title="Unstaged">U</span>`},
		"unstaged.txt": {`title="Unstaged">U</span>`},
		"new name.txt": {`title="Staged">S</span>`},
		"binary.dat":   {`title="Untracked">?</span>`},
	} {
		start := strings.Index(allNav, `title="`+name+`"`)
		if start < 0 {
			t.Fatalf("navigation link missing for %s", name)
		}
		end := strings.Index(allNav[start:], "</a>")
		link := allNav[start : start+end]
		for _, label := range want {
			if !strings.Contains(link, label) {
				t.Errorf("%s should show %s in file navigation: %s", name, label, link)
			}
		}
	}
}

func TestWorkingViewsShowUntrackedSymlinksAsAddedLinkTargets(t *testing.T) {
	for _, tc := range []struct {
		name, target string
		prepare      func(t *testing.T) string
	}{
		{
			name: "directory link",
			prepare: func(t *testing.T) string {
				outside := t.TempDir()
				put(t, outside, "secret.txt", "must not appear\n")
				return outside
			},
		},
		{
			name:   "file link",
			target: "base.txt",
		},
		{
			name:   "dangling link",
			target: "missing.txt",
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			dir := repo(t)
			put(t, dir, "base.txt", "base\n")
			commit(t, dir, "base")
			target := tc.target
			if tc.prepare != nil {
				target = tc.prepare(t)
			}
			if err := os.Symlink(target, filepath.Join(dir, "link")); err != nil {
				t.Fatal(err)
			}

			app, err := inspectApp(dir, "main")
			if err != nil {
				t.Fatal(err)
			}
			if app.Uncommitted == nil {
				t.Fatal("uncommitted view missing")
			}
			for _, view := range []View{*app.Uncommitted, app.AllChanges} {
				file, ok := fileByPath(view.Files, "link")
				if !ok || file.Status != "added" || !file.Untracked || file.Additions != 1 || len(file.Hunks) != 1 || len(file.Hunks[0].Lines) != 1 {
					t.Fatalf("symlink should be one added line: %+v", file)
				}
				line := file.Hunks[0].Lines[0]
				if line.Kind != "add" || line.Text != target {
					t.Fatalf("symlink content should be its target %q: %+v", target, line)
				}
				if len(view.Files) != 1 {
					t.Fatalf("link target contents should not appear: %+v", view.Files)
				}
			}
		})
	}
}

func TestWorkingViewsListUntrackedNestedRepositoryWithoutContents(t *testing.T) {
	dir := repo(t)
	put(t, dir, "base.txt", "base\n")
	commit(t, dir, "base")
	nested := filepath.Join(dir, "nested")
	if err := os.Mkdir(nested, 0755); err != nil {
		t.Fatal(err)
	}
	gitTest(t, nested, "init", "-q")
	put(t, nested, "inside.txt", "must not appear\n")

	app, err := inspectApp(dir, "main")
	if err != nil {
		t.Fatal(err)
	}
	if app.Uncommitted == nil {
		t.Fatal("uncommitted view missing")
	}
	for _, view := range []View{*app.Uncommitted, app.AllChanges} {
		file, ok := fileByPath(view.Files, "nested/")
		if !ok || file.Status != "added" || !file.Untracked || len(file.Hunks) != 0 || len(view.Files) != 1 {
			t.Fatalf("nested repository should appear without contents: %+v", view.Files)
		}
	}
}

func TestRootCommitCanBeInspectedAgainstEmptyTree(t *testing.T) {
	dir := repo(t)
	put(t, dir, "initial.txt", "created\n")
	commit(t, dir, "initial commit")
	sha := gitTest(t, dir, "rev-parse", "HEAD")
	item, err := inspectCommit(dir, sha)
	if err != nil {
		t.Fatal(err)
	}
	if item.Parent != "" || len(item.Diff.Files) != 1 || item.Diff.Files[0].Path != "initial.txt" || item.Diff.Files[0].Status != "added" {
		t.Fatalf("root commit detail incorrect: %+v", item)
	}
}

func TestSnapshotContainsAllViewsWithoutRuntimeNetworkDependency(t *testing.T) {
	dir := repo(t)
	put(t, dir, "base.txt", "base\n")
	commit(t, dir, "base")
	gitTest(t, dir, "checkout", "-qb", "feature")
	put(t, dir, "committed.txt", "committed\n")
	commit(t, dir, "feature change")
	put(t, dir, "untracked.txt", "untracked\n")
	app, err := inspectApp(dir, "main")
	if err != nil {
		t.Fatal(err)
	}
	page, err := renderApp(app)
	if err != nil {
		t.Fatal(err)
	}
	html := string(page)
	for _, label := range []string{"Files changed", "Commits", "All changes", "Uncommitted changes", "feature change", "untracked.txt", "Split", "Unified"} {
		if !strings.Contains(html, label) {
			t.Errorf("snapshot misses %q", label)
		}
	}
	if strings.Contains(html, "http://") || strings.Contains(html, "https://") || strings.Contains(html, "data-watch-events=") {
		t.Fatal("snapshot must remain network-free")
	}
}
