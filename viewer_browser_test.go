package main

import (
	"fmt"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// Run with ACT_AS_PR_BROWSER_TEST=1 go test -run TestFileHeadingFollowsVisibleDiff.
func TestFileHeadingFollowsVisibleDiff(t *testing.T) {
	if os.Getenv("ACT_AS_PR_BROWSER_TEST") != "1" {
		t.Skip("browser test is opt in")
	}
	if _, err := exec.LookPath("browser-use"); err != nil {
		t.Skip("browser-use is unavailable")
	}
	lines := make([]Line, 100)
	for i := range lines {
		lines[i] = Line{Kind: "add", Text: fmt.Sprintf("line %d", i+1), New: i + 1}
	}
	view := View{Repository: "example", Files: []File{
		{Path: "src/first.go", Status: "modified", Hunks: []Hunk{{Header: "@@ -0,0 +1,100 @@", Lines: lines}}},
		{Path: "src/second.go", Status: "modified", Hunks: []Hunk{{Header: "@@ -0,0 +1,100 @@", Lines: lines}}},
	}}
	files := view
	files.Files = append([]File(nil), view.Files...)
	finishView(&files, "files")
	all := view
	all.Files = append([]File(nil), view.Files...)
	finishView(&all, "all")
	commit := view
	commit.Files = append([]File(nil), view.Files...)
	finishView(&commit, "commit-1")
	page, err := renderApp(App{Repository: "example", FilesChanged: files, AllChanges: all, Commits: []Commit{{SHA: "abc123", Subject: "change files", Diff: commit}}})
	if err != nil {
		t.Fatal(err)
	}
	file := filepath.Join(t.TempDir(), "preview.html")
	if err := os.WriteFile(file, page, 0600); err != nil {
		t.Fatal(err)
	}
	args := []string{"--session", "act-as-pr-sticky-test"}
	runBrowser := func(command, value string) string {
		t.Helper()
		cmd := exec.Command("browser-use", append(args, command, value)...)
		output, err := cmd.CombinedOutput()
		if err != nil {
			t.Fatalf("browser-use %s: %v: %s", command, err, output)
		}
		return strings.TrimSpace(string(output))
	}
	defer exec.Command("browser-use", append(args, "close")...).Run()
	runBrowser("open", (&url.URL{Scheme: "file", Path: file}).String())
	for _, pane := range []string{"files", "all", "commits"} {
		js := fmt.Sprintf(`(() => {
			document.querySelector('[data-tab="%s"]').click();
			if ('%s' === 'commits') document.querySelector('[data-commit-target="abc123"]').click();
			const scope = '%s' === 'commits' ? document.querySelector('[data-commit-detail="abc123"]') : document.querySelector('[data-view="%s"]');
			const cards = [...scope.querySelectorAll('.file-card')];
			const y = el => el.getBoundingClientRect().top + scrollY;
			const first = cards[0].querySelector('.file-header');
			const second = cards[1].querySelector('.file-header');
			window.scrollTo({top:y(cards[0]) + 150, behavior:'instant'});
			if (Math.abs(first.getBoundingClientRect().top) > 2) return 'FAIL: first path did not stay at top ' + JSON.stringify({top:first.getBoundingClientRect().top, scrollY, card:y(cards[0]), height:innerHeight, scrollHeight:document.documentElement.scrollHeight, position:getComputedStyle(first).position, overflow:getComputedStyle(cards[0]).overflow});
			if (first.querySelector('.file-path').textContent !== 'src/first.go' || !first.querySelector('.copy-path')) return 'FAIL: first path or copy control is missing';
			first.querySelector('.copy-path').click();
			if (!cards[0].open) return 'FAIL: copying collapsed the file';
			first.querySelector('.viewed-toggle').click();
			if (cards[0].open) return 'FAIL: Viewed did not collapse the file';
			first.querySelector('.viewed-toggle').click();
			if (!cards[0].open) return 'FAIL: unmarking Viewed did not reopen the file';
			window.scrollTo({top:y(cards[0]) + 150, behavior:'instant'});
			window.scrollTo({top:y(cards[1]) + 150, behavior:'instant'});
			if (Math.abs(second.getBoundingClientRect().top) > 2) return 'FAIL: second path did not replace first';
			if (first.getBoundingClientRect().bottom > 2) return 'FAIL: previous path remains visible';
			return 'PASS';
		})()`, pane, pane, pane, pane)
		if output := runBrowser("eval", js); !strings.Contains(output, "PASS") || strings.Contains(output, "FAIL:") {
			t.Errorf("%s heading behavior: %s", pane, output)
		}
		if dir := os.Getenv("ACT_AS_PR_SCREENSHOT_DIR"); dir != "" {
			time.Sleep(2100 * time.Millisecond)
			path := filepath.Join(dir, pane+"-sticky-header.png")
			cmd := exec.Command("browser-use", append(args, "screenshot", path)...)
			if output, err := cmd.CombinedOutput(); err != nil {
				t.Errorf("save %s screenshot: %v: %s", pane, err, output)
			}
		}
	}
	runBrowser("python", "browser._run(browser._session._cdp_set_viewport(390, 844))")
	if output := runBrowser("eval", `(() => {
		const card = document.querySelector('[data-commit-detail="abc123"] .file-card');
		const header = card.querySelector('.file-header');
		window.scrollTo({top:card.getBoundingClientRect().top + scrollY + 150, behavior:'instant'});
		if (Math.abs(header.getBoundingClientRect().top) > 2) return 'FAIL: narrow viewport heading did not stay at top';
		if (document.documentElement.scrollWidth > document.documentElement.clientWidth + 1) return 'FAIL: narrow viewport page overflows horizontally';
		if (header.querySelector('.copy-path').getBoundingClientRect().right > innerWidth) return 'FAIL: copy button is outside the viewport';
		return 'PASS';
	})()`); !strings.Contains(output, "PASS") || strings.Contains(output, "FAIL:") {
		t.Errorf("narrow viewport heading behavior: %s", output)
	}
	if dir := os.Getenv("ACT_AS_PR_SCREENSHOT_DIR"); dir != "" {
		path := filepath.Join(dir, "narrow-sticky-header.png")
		cmd := exec.Command("browser-use", append(args, "screenshot", path)...)
		if output, err := cmd.CombinedOutput(); err != nil {
			t.Errorf("save narrow screenshot: %v: %s", err, output)
		}
	}
}
