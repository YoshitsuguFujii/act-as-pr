package main

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"os/signal"
	"sync"
	"syscall"
	"time"
)

type WatchServer struct {
	listener net.Listener
	server   *http.Server
	token    string
	root     string
	base     string
	interval time.Duration

	mu          sync.RWMutex
	page        []byte
	version     string
	subscribers map[chan string]struct{}
	cancel      context.CancelFunc
	done        chan struct{}
	closeOnce   sync.Once
}

func startWatch(dir, base string, interval time.Duration) (*WatchServer, error) {
	if interval <= 0 {
		return nil, errors.New("watch interval must be positive")
	}
	app, err := inspectApp(dir, base)
	if err != nil {
		return nil, err
	}
	secret := make([]byte, 32)
	if _, err := rand.Read(secret); err != nil {
		return nil, err
	}
	token := base64.RawURLEncoding.EncodeToString(secret)
	version, err := appVersion(app)
	if err != nil {
		return nil, err
	}
	page, err := renderWatchApp(app, "/"+token+"/events", version)
	if err != nil {
		return nil, err
	}
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return nil, err
	}
	ctx, cancel := context.WithCancel(context.Background())
	w := &WatchServer{listener: listener, token: token, root: app.Root, base: base, interval: interval, page: page, version: version, subscribers: make(map[chan string]struct{}), cancel: cancel, done: make(chan struct{})}
	w.server = &http.Server{Handler: http.HandlerFunc(w.serveHTTP), ReadHeaderTimeout: 5 * time.Second}
	go func() { _ = w.server.Serve(listener) }()
	go w.pollLoop(ctx)
	return w, nil
}

func appVersion(app App) (string, error) {
	data, err := json.Marshal(app)
	if err != nil {
		return "", err
	}
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:]), nil
}

func (w *WatchServer) URL() string {
	return "http://" + w.listener.Addr().String() + "/" + w.token + "/"
}

func (w *WatchServer) serveHTTP(out http.ResponseWriter, request *http.Request) {
	if request.Host != w.listener.Addr().String() {
		http.NotFound(out, request)
		return
	}
	pagePath := "/" + w.token + "/"
	eventPath := pagePath + "events"
	if request.URL.Path != pagePath && request.URL.Path != eventPath {
		http.NotFound(out, request)
		return
	}
	if request.Method != http.MethodGet && request.Method != http.MethodHead {
		out.Header().Set("Allow", "GET, HEAD")
		http.Error(out, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	out.Header().Set("Cache-Control", "no-store")
	out.Header().Set("X-Content-Type-Options", "nosniff")
	out.Header().Set("Referrer-Policy", "no-referrer")
	out.Header().Set("X-Frame-Options", "DENY")
	if request.URL.Path == pagePath {
		w.mu.RLock()
		page := w.page
		w.mu.RUnlock()
		out.Header().Set("Content-Type", "text/html; charset=utf-8")
		_, _ = out.Write(page)
		return
	}
	out.Header().Set("Content-Type", "text/event-stream")
	if request.Method == http.MethodHead {
		return
	}
	flusher, ok := out.(http.Flusher)
	if !ok {
		http.Error(out, "streaming unsupported", http.StatusInternalServerError)
		return
	}
	updates := make(chan string, 1)
	w.mu.Lock()
	w.subscribers[updates] = struct{}{}
	version := w.version
	w.mu.Unlock()
	defer func() {
		w.mu.Lock()
		delete(w.subscribers, updates)
		w.mu.Unlock()
	}()
	if _, err := fmt.Fprintf(out, "event: version\ndata: %s\n\n", version); err != nil {
		return
	}
	flusher.Flush()
	for {
		select {
		case version := <-updates:
			if _, err := fmt.Fprintf(out, "event: version\ndata: %s\n\n", version); err != nil {
				return
			}
			flusher.Flush()
		case <-request.Context().Done():
			return
		}
	}
}

func (w *WatchServer) pollOnce() (bool, error) {
	app, err := inspectApp(w.root, w.base)
	if err != nil {
		return false, err
	}
	version, err := appVersion(app)
	if err != nil {
		return false, err
	}
	w.mu.RLock()
	unchanged := version == w.version
	w.mu.RUnlock()
	if unchanged {
		return false, nil
	}
	page, err := renderWatchApp(app, "/"+w.token+"/events", version)
	if err != nil {
		return false, err
	}
	w.mu.Lock()
	if version == w.version {
		w.mu.Unlock()
		return false, nil
	}
	w.page, w.version = page, version
	for subscriber := range w.subscribers {
		select {
		case subscriber <- version:
		default:
			select {
			case <-subscriber:
			default:
			}
			subscriber <- version
		}
	}
	w.mu.Unlock()
	return true, nil
}

func (w *WatchServer) pollLoop(ctx context.Context) {
	defer close(w.done)
	ticker := time.NewTicker(w.interval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			_, _ = w.pollOnce()
		}
	}
}

func (w *WatchServer) Close() {
	w.closeOnce.Do(func() {
		w.cancel()
		ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
		defer cancel()
		_ = w.server.Shutdown(ctx)
		<-w.done
	})
}

func runWatch(dir, base string, out io.Writer) error {
	w, err := startWatch(dir, base, 750*time.Millisecond)
	if err != nil {
		return err
	}
	defer w.Close()
	if err := openBrowser(w.URL()); err != nil {
		return err
	}
	fmt.Fprintf(out, "Watching local PR:\n%s\nPress Ctrl-C to stop.\n", w.URL())
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	<-ctx.Done()
	return nil
}
