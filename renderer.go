package main

import (
	"crypto/sha256"
	"encoding/base64"
	"fmt"
	"html/template"
)

const maxHTMLBytes = 128 << 20

type pageData struct {
	App
	CSS         template.CSS
	JS          template.JS
	CSP         string
	WatchEvents string
	Version     string
}

func render(view View) ([]byte, error) {
	files := view
	files.Files = append([]File(nil), view.Files...)
	finishView(&files, "files")
	all := view
	all.Files = append([]File(nil), view.Files...)
	finishView(&all, "all")
	return renderApp(App{Repository: view.Repository, Root: view.Root, Base: view.Base, Head: view.Head, MergeBase: view.MergeBase, FilesChanged: files, AllChanges: all})
}

func renderApp(app App) ([]byte, error) {
	return renderPage(app, "", "")
}

func renderWatchApp(app App, eventsPath, version string) ([]byte, error) {
	return renderPage(app, eventsPath, version)
}

func renderPage(app App, eventsPath, version string) ([]byte, error) {
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
	cssHash, jsHash := sha256.Sum256(css), sha256.Sum256(js)
	csp := fmt.Sprintf("default-src 'none'; style-src 'sha256-%s'; script-src 'sha256-%s'; img-src data:; base-uri 'none'; form-action 'none'", base64.StdEncoding.EncodeToString(cssHash[:]), base64.StdEncoding.EncodeToString(jsHash[:]))
	if eventsPath != "" {
		csp += "; connect-src 'self'"
	}
	data := pageData{App: app, CSS: template.CSS(css), JS: template.JS(js), CSP: csp, WatchEvents: eventsPath, Version: version}
	tmpl, err := template.New("viewer").Parse(string(htmlBytes))
	if err != nil {
		return nil, err
	}
	out := &limitBuffer{max: maxHTMLBytes}
	if err := tmpl.Execute(out, data); err != nil {
		return nil, err
	}
	return out.Bytes(), nil
}
