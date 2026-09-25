# act-as-pr

`act-as-pr` renders your local Git changes as if they were a GitHub pull request. It is a read-only preview: it does not create a real PR or send review data anywhere.

![act-as-pr Files changed preview](docs/images/files-changed.png)

## Install

Requires Go 1.23 or newer and Git. From this directory:

```sh
go install .
```

Ensure `$(go env GOPATH)/bin` (or your `GOBIN`) is on `PATH`. To build a standalone binary instead:

```sh
go build -o act-as-pr .
```

## Use

Run from any directory inside a Git repository:

```sh
act-as-pr main
act-as-pr master
act-as-pr HEAD~3
```

The argument is required. `act-as-pr` finds the merge base of the supplied revision and `HEAD`, then previews `git diff <merge-base> HEAD`. This includes committed changes on the current branch; uncommitted working tree and staged changes are not included.

The command writes one self-contained HTML file under the OS temporary directory and opens it with the macOS default browser (`xdg-open` on Linux). There is no local server, Node runtime, network request, or process to stop. The page supports Unified and Split diff views, file navigation, folding, line numbers, and the system light/dark theme.

## Safety

Git is invoked with argument arrays for read-only commands. The normal viewer path does not write to the repository, index, refs, remotes, or Git config. Repository paths and diff text are HTML-escaped; the page uses a restrictive Content Security Policy and contains its CSS and JavaScript locally. No authentication, telemetry, GitHub API, or external assets are used.

## Limits

- The Git patch output is limited to 32 MiB so unusually large diffs fail with an error instead of exhausting memory. Both display modes are included in the HTML, so large previews can still take time to render.
- Binary changes display a placeholder; binary content is not decoded.
- Syntax highlighting covers common source file extensions and a small keyword set. It is intentionally lightweight.
- The OS needs a configured default browser for automatic opening. If opening fails, the command reports the saved HTML path.
- Windows browser opening is not implemented.
