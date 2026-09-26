package main

import (
	"bytes"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
)

const (
	maxCommits             = 200
	maxUntrackedFiles      = 1000
	maxFilesPerView        = 2000
	maxAggregatePatchBytes = 32 << 20
)

type Commit struct {
	SHA, ShortSHA, Parent, Subject, Author, Timestamp string
	Diff                                              View
}

type App struct {
	Repository, Root, Base, Head, MergeBase string
	FilesChanged                            View
	AllChanges                              View
	Uncommitted                             *View
	Commits                                 []Commit
	StagedCount, UnstagedCount              int
	UntrackedCount                          int
}

func inspectApp(dir, base string) (App, error) {
	var app App
	committed, err := inspect(dir, base)
	if err != nil {
		return app, err
	}
	app.Repository, app.Root, app.Base, app.Head, app.MergeBase = committed.Repository, committed.Root, committed.Base, committed.Head, committed.MergeBase
	app.FilesChanged = committed
	finishView(&app.FilesChanged, "files")
	budget := committed.patchBytes

	commitRecords, err := listPRCommits(app.Root, app.MergeBase)
	if err != nil {
		return app, err
	}
	for i, record := range commitRecords {
		item, err := commitWithDiff(app.Root, record, fmt.Sprintf("commit-%d", i+1))
		if err != nil {
			return app, err
		}
		budget += item.Diff.patchBytes
		if budget > maxAggregatePatchBytes {
			return app, fmt.Errorf("preview exceeds %d MiB aggregate diff limit", maxAggregatePatchBytes>>20)
		}
		app.Commits = append(app.Commits, item)
	}

	untracked, err := untrackedPaths(app.Root)
	if err != nil {
		return app, err
	}
	app.UntrackedCount = len(untracked)
	staged, err := git(app.Root, "diff", "--no-ext-diff", "--no-textconv", "--cached", "--name-only", "-z", "HEAD", "--")
	if err != nil {
		return app, err
	}
	unstaged, err := git(app.Root, "diff", "--no-ext-diff", "--no-textconv", "--name-only", "-z", "--")
	if err != nil {
		return app, err
	}
	stagedPaths, err := nulPaths(staged)
	if err != nil {
		return app, err
	}
	unstagedPaths, err := nulPaths(unstaged)
	if err != nil {
		return app, err
	}
	app.StagedCount, app.UnstagedCount = len(stagedPaths), len(unstagedPaths)

	working, _, err := inspectDiff(app.Root, "HEAD", "", "working")
	if err != nil {
		return app, err
	}
	all, _, err := inspectDiff(app.Root, app.MergeBase, "", "all")
	if err != nil {
		return app, err
	}
	budget += working.patchBytes + all.patchBytes
	if budget > maxAggregatePatchBytes {
		return app, fmt.Errorf("preview exceeds %d MiB aggregate diff limit", maxAggregatePatchBytes>>20)
	}
	for _, name := range untracked {
		file, size, err := inspectUntracked(app.Root, name)
		if err != nil {
			return app, err
		}
		budget += size * 2
		if budget > maxAggregatePatchBytes {
			return app, fmt.Errorf("preview exceeds %d MiB aggregate diff limit", maxAggregatePatchBytes>>20)
		}
		working.Files = append(working.Files, file)
		all.Files = append(all.Files, file)
	}
	if len(working.Files) > maxFilesPerView || len(all.Files) > maxFilesPerView {
		return app, fmt.Errorf("preview exceeds %d files per view", maxFilesPerView)
	}
	markWorkingState(working.Files, stagedPaths, unstagedPaths, untracked)
	markWorkingState(all.Files, stagedPaths, unstagedPaths, untracked)
	finishView(&working, "working")
	finishView(&all, "all")
	working.Repository, working.Root, working.Base, working.Head = app.Repository, app.Root, "HEAD", "working state"
	all.Repository, all.Root, all.Base, all.Head = app.Repository, app.Root, app.Base, "working state"
	if len(working.Files) > 0 {
		app.Uncommitted = &working
	}
	app.AllChanges = all
	return app, nil
}

func markWorkingState(files []File, staged, unstaged, untracked []string) {
	toSet := func(paths []string) map[string]bool {
		set := make(map[string]bool, len(paths))
		for _, name := range paths {
			set[name] = true
		}
		return set
	}
	stagedSet, unstagedSet, untrackedSet := toSet(staged), toSet(unstaged), toSet(untracked)
	for i := range files {
		files[i].Staged = stagedSet[files[i].Path]
		files[i].Unstaged = unstagedSet[files[i].Path]
		files[i].Untracked = untrackedSet[files[i].Path]
	}
}

func inspectDiff(root, from, to, prefix string) (View, int, error) {
	args := []string{"diff", "--no-ext-diff", "--no-textconv", "--find-renames", "--name-status", "-z", from}
	if to != "" {
		args = append(args, to)
	}
	args = append(args, "--")
	names, err := git(root, args...)
	if err != nil {
		return View{}, 0, err
	}
	args = []string{"diff", "--no-ext-diff", "--no-textconv", "--no-color", "--find-renames", "--patch", "--src-prefix=a/", "--dst-prefix=b/", from}
	if to != "" {
		args = append(args, to)
	}
	args = append(args, "--")
	patch, err := git(root, args...)
	if err != nil {
		return View{}, 0, err
	}
	v, err := parseDiffOutput(names, patch, prefix)
	return v, len(patch), err
}

func parseDiffOutput(names, patch []byte, prefix string) (View, error) {
	var view View
	var err error
	view.Files, err = parseNames(names)
	if err != nil {
		return view, err
	}
	sections := splitPatch(string(patch))
	if len(sections) != len(view.Files) {
		return view, fmt.Errorf("diff metadata/patch mismatch: %d files, %d patches", len(view.Files), len(sections))
	}
	for i := range view.Files {
		parsePatch(&view.Files[i], sections[i])
		view.Files[i].Split = splitRows(view.Files[i].Hunks)
	}
	view.patchBytes = len(patch)
	finishView(&view, prefix)
	return view, nil
}

func finishView(view *View, prefix string) {
	view.ID = prefix
	view.Additions, view.Deletions = 0, 0
	for i := range view.Files {
		view.Files[i].ID = fmt.Sprintf("%s-file-%d", prefix, i+1)
		view.Additions += view.Files[i].Additions
		view.Deletions += view.Files[i].Deletions
	}
	view.NavGroups = groupNavigation(view.Files)
}

type commitRecord struct {
	sha, parents, author, timestamp, subject string
}

func parseCommitRecords(raw []byte) ([]commitRecord, error) {
	if len(raw) == 0 {
		return nil, nil
	}
	if raw[len(raw)-1] != 0 {
		return nil, errors.New("malformed Git commit list")
	}
	fields := bytes.Split(raw[:len(raw)-1], []byte{0})
	if len(fields)%5 != 0 {
		return nil, errors.New("malformed Git commit metadata")
	}
	var records []commitRecord
	for i := 0; i < len(fields); i += 5 {
		records = append(records, commitRecord{string(fields[i]), string(fields[i+1]), string(fields[i+2]), string(fields[i+3]), string(fields[i+4])})
	}
	return records, nil
}

func listPRCommits(root, mergeBase string) ([]commitRecord, error) {
	rangeArg := mergeBase + "..HEAD"
	countRaw, err := git(root, "rev-list", "--count", rangeArg)
	if err != nil {
		return nil, err
	}
	count, err := strconv.Atoi(strings.TrimSpace(string(countRaw)))
	if err != nil {
		return nil, fmt.Errorf("invalid Git commit count: %w", err)
	}
	if count > maxCommits {
		return nil, fmt.Errorf("PR range exceeds %d commit limit", maxCommits)
	}
	if count == 0 {
		return nil, nil
	}
	raw, err := git(root, "log", "--reverse", "-z", "--format=%H%x00%P%x00%an%x00%aI%x00%s", rangeArg)
	if err != nil {
		return nil, err
	}
	return parseCommitRecords(raw)
}

func inspectCommit(root, revision string) (Commit, error) {
	resolved, err := git(root, "rev-parse", "--verify", "--end-of-options", revision+"^{commit}")
	if err != nil {
		return Commit{}, err
	}
	raw, err := git(root, "log", "-1", "-z", "--format=%H%x00%P%x00%an%x00%aI%x00%s", strings.TrimSpace(string(resolved)))
	if err != nil {
		return Commit{}, err
	}
	records, err := parseCommitRecords(raw)
	if err != nil || len(records) != 1 {
		return Commit{}, fmt.Errorf("unable to read commit metadata: %w", err)
	}
	return commitWithDiff(root, records[0], "commit")
}

func commitWithDiff(root string, record commitRecord, prefix string) (Commit, error) {
	item := Commit{SHA: record.sha, Subject: record.subject, Author: record.author, Timestamp: record.timestamp}
	if len(item.SHA) > 8 {
		item.ShortSHA = item.SHA[:8]
	} else {
		item.ShortSHA = item.SHA
	}
	parents := strings.Fields(record.parents)
	if len(parents) > 0 {
		item.Parent = parents[0]
		view, _, err := inspectDiff(root, item.Parent, item.SHA, prefix)
		if err != nil {
			return item, err
		}
		item.Diff = view
	} else {
		view, err := inspectRootDiff(root, item.SHA, prefix)
		if err != nil {
			return item, err
		}
		item.Diff = view
	}
	item.Diff.Root, item.Diff.Base, item.Diff.Head = root, item.Parent, item.SHA
	return item, nil
}

func inspectRootDiff(root, sha, prefix string) (View, error) {
	names, err := git(root, "diff-tree", "--root", "-r", "--no-commit-id", "--no-ext-diff", "--no-textconv", "--find-renames", "--name-status", "-z", sha)
	if err != nil {
		return View{}, err
	}
	patch, err := git(root, "diff-tree", "--root", "-r", "--no-commit-id", "--no-ext-diff", "--no-textconv", "--no-color", "--find-renames", "--patch", "--src-prefix=a/", "--dst-prefix=b/", sha)
	if err != nil {
		return View{}, err
	}
	return parseDiffOutput(names, patch, prefix)
}

func nulPaths(raw []byte) ([]string, error) {
	if len(raw) == 0 {
		return nil, nil
	}
	if raw[len(raw)-1] != 0 {
		return nil, errors.New("malformed NUL-delimited Git paths")
	}
	fields := bytes.Split(raw[:len(raw)-1], []byte{0})
	paths := make([]string, 0, len(fields))
	for _, field := range fields {
		paths = append(paths, string(field))
	}
	return paths, nil
}

func untrackedPaths(root string) ([]string, error) {
	raw, err := git(root, "ls-files", "--others", "--exclude-standard", "-z", "--")
	if err != nil {
		return nil, err
	}
	paths, err := nulPaths(raw)
	if err != nil {
		return nil, err
	}
	if len(paths) > maxUntrackedFiles {
		return nil, fmt.Errorf("preview exceeds %d untracked file limit", maxUntrackedFiles)
	}
	return paths, nil
}

func inspectUntracked(root, name string) (File, int, error) {
	file := File{Path: name, Status: "added"}
	if !filepath.IsLocal(name) {
		return file, 0, fmt.Errorf("unsafe untracked path %q", name)
	}
	path := filepath.Join(root, name)
	info, err := os.Lstat(path)
	if err != nil {
		return file, 0, err
	}
	if info.Mode()&os.ModeSymlink != 0 {
		target, err := os.Readlink(path)
		if err != nil {
			return file, 0, err
		}
		// git diff --no-index はディレクトリへのリンクを辿って <パス>/null を探し失敗するため、リンク先文字列を直接表示する。
		line := Line{Kind: "add", Text: target, New: 1}
		file.Hunks = []Hunk{{Header: "@@ -0,0 +1 @@", Lines: []Line{line}}}
		file.Additions = 1
		file.Split = splitRows(file.Hunks)
		return file, len(target), nil
	}
	if info.IsDir() {
		return file, 0, nil
	}
	patch, err := gitWithAllowedExit(root, true, "diff", "--no-index", "--no-ext-diff", "--no-textconv", "--no-color", "--patch", "--", "/dev/null", name)
	if err != nil {
		return file, 0, err
	}
	sections := splitPatch(string(patch))
	if len(sections) > 1 {
		return file, 0, fmt.Errorf("unexpected untracked diff for %q", name)
	}
	if len(sections) == 1 {
		parsePatch(&file, sections[0])
		file.Split = splitRows(file.Hunks)
	}
	return file, len(patch), nil
}
