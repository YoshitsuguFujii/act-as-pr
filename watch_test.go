package main

import (
	"bufio"
	"context"
	"io"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestWatchServerUsesLoopbackEphemeralPortAndSessionToken(t *testing.T) {
	dir := repo(t)
	put(t, dir, "base.txt", "base\n")
	commit(t, dir, "base")
	server, err := startWatch(dir, "main", time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { server.Close() })
	address, ok := server.listener.Addr().(*net.TCPAddr)
	if !ok || !address.IP.Equal(net.ParseIP("127.0.0.1")) || address.Port == 0 {
		t.Fatalf("watch server must use loopback and an assigned port: %v", server.listener.Addr())
	}
	if len(server.token) < 40 || !strings.HasPrefix(server.URL(), "http://127.0.0.1:") {
		t.Fatalf("session URL is not protected: %s", server.URL())
	}
	client := &http.Client{Timeout: 2 * time.Second}
	for _, path := range []string{"/", "/events", "/wrong-token/", "/wrong-token/events"} {
		response, err := client.Get("http://" + address.String() + path)
		if err != nil {
			t.Fatal(err)
		}
		response.Body.Close()
		if response.StatusCode != http.StatusNotFound {
			t.Errorf("unprotected path %s returned %d", path, response.StatusCode)
		}
	}
	response, err := client.Get(server.URL())
	if err != nil {
		t.Fatal(err)
	}
	page, err := io.ReadAll(response.Body)
	response.Body.Close()
	if err != nil || response.StatusCode != http.StatusOK || !strings.Contains(string(page), "Files changed") {
		t.Fatalf("protected page unavailable: status=%d err=%v", response.StatusCode, err)
	}
	if response.Header.Get("Cache-Control") != "no-store" || response.Header.Get("Access-Control-Allow-Origin") != "" || !strings.Contains(string(page), "connect-src &#39;self&#39;") {
		t.Fatal("watch page security headers or CSP missing")
	}
	post, err := client.Post(server.URL(), "text/plain", strings.NewReader("ignored"))
	if err != nil {
		t.Fatal(err)
	}
	post.Body.Close()
	if post.StatusCode != http.StatusMethodNotAllowed {
		t.Fatalf("mutation method returned %d", post.StatusCode)
	}
	if got := gitTest(t, dir, "status", "--porcelain=v1"); got != "" {
		t.Fatalf("watch requests changed repository: %s", got)
	}
}

func TestWatchNotifiesRepeatedEditsAndCommitWithoutPageRestart(t *testing.T) {
	dir := repo(t)
	put(t, dir, "tracked.txt", "base\n")
	commit(t, dir, "base")
	gitTest(t, dir, "checkout", "-qb", "feature")
	put(t, dir, "committed.txt", "first commit\n")
	commit(t, dir, "first")
	put(t, dir, "tracked.txt", "first edit\n")
	server, err := startWatch(dir, "main", time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { server.Close() })
	if changed, err := server.pollOnce(); err != nil || changed {
		t.Fatalf("unchanged repository triggered update: changed=%t err=%v", changed, err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, server.URL()+"events", nil)
	if err != nil {
		t.Fatal(err)
	}
	response, err := (&http.Client{}).Do(request)
	if err != nil {
		t.Fatal(err)
	}
	defer response.Body.Close()
	if response.Header.Get("Content-Type") != "text/event-stream" {
		t.Fatalf("SSE content type: %s", response.Header.Get("Content-Type"))
	}
	lines := bufio.NewReader(response.Body)
	for i := 0; i < 3; i++ {
		if _, err := lines.ReadString('\n'); err != nil {
			t.Fatal(err)
		}
	}
	oldVersion := server.version
	for _, content := range []string{"second edit\n", "third edit\n"} {
		put(t, dir, "tracked.txt", content)
		if changed, err := server.pollOnce(); err != nil || !changed {
			t.Fatalf("repeated edit not detected: changed=%t err=%v", changed, err)
		}
	}
	if server.version == oldVersion {
		t.Fatal("content updates did not change version")
	}
	if changed, err := server.pollOnce(); err != nil || changed {
		t.Fatalf("same content triggered update: changed=%t err=%v", changed, err)
	}
	put(t, dir, "untracked.txt", "new file\n")
	if changed, err := server.pollOnce(); err != nil || !changed {
		t.Fatalf("new file not detected: changed=%t err=%v", changed, err)
	}
	gitTest(t, dir, "add", "tracked.txt", "untracked.txt")
	if changed, err := server.pollOnce(); err != nil || !changed {
		t.Fatalf("staging not detected: changed=%t err=%v", changed, err)
	}
	gitTest(t, dir, "commit", "-qm", "second")
	if changed, err := server.pollOnce(); err != nil || !changed {
		t.Fatalf("new commit not detected: changed=%t err=%v", changed, err)
	}
	page, err := http.Get(server.URL())
	if err != nil {
		t.Fatal(err)
	}
	body, err := io.ReadAll(page.Body)
	page.Body.Close()
	if err != nil || !strings.Contains(string(body), "second") || strings.Contains(string(body), "Uncommitted changes <span") {
		t.Fatal("watch page did not reflect the new commit and cleared worktree")
	}

	eventRead := make(chan string, 1)
	go func() {
		for {
			line, err := lines.ReadString('\n')
			if err != nil {
				eventRead <- ""
				return
			}
			if strings.HasPrefix(line, "data: ") {
				eventRead <- strings.TrimSpace(strings.TrimPrefix(line, "data: "))
				return
			}
		}
	}()
	select {
	case version := <-eventRead:
		if version == "" || version == oldVersion {
			t.Fatalf("SSE did not carry a new version: %q", version)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("SSE update timed out")
	}
	if got := gitTest(t, dir, "status", "--porcelain=v1"); got != "" {
		t.Fatalf("watcher changed repository after test commit: %s", got)
	}
	if _, err := os.Stat(filepath.Join(dir, "untracked.txt")); err != nil {
		t.Fatal(err)
	}
}
