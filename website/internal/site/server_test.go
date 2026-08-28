package site

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestRoutes(t *testing.T) {
	t.Parallel()

	tests := []struct {
		path  string
		title string
	}{
		{path: "/", title: "Perk Workbench"},
		{path: "/demo", title: "Live demo"},
		{path: "/docs", title: "Documentation"},
		{path: "/docs/getting-started", title: "Getting started"},
		{path: "/docs/connections", title: "Connections"},
		{path: "/docs/workspace", title: "Workspace"},
		{path: "/docs/ai", title: "AI assistance"},
		{path: "/docs/plugins", title: "Plugins"},
	}
	server := New("test")
	for _, tt := range tests {
		t.Run(tt.path, func(t *testing.T) {
			req := httptest.NewRequest("GET", tt.path, nil)
			recorder := httptest.NewRecorder()
			server.ServeHTTP(recorder, req)

			if recorder.Code != 200 {
				t.Fatalf("status = %d, want 200", recorder.Code)
			}
			if got := recorder.Header().Get("Content-Type"); !strings.HasPrefix(got, "text/html") {
				t.Fatalf("content type = %q, want text/html", got)
			}
			body := recorder.Body.String()
			if !strings.Contains(body, "<title>"+tt.title+" · Perk Workbench</title>") {
				t.Errorf("body does not contain title %q", tt.title)
			}
			if !strings.Contains(body, `aria-current="page"`) {
				t.Errorf("body does not contain aria-current page marker")
			}
		})
	}
}

func TestDocumentationNavigation(t *testing.T) {
	t.Parallel()

	server := New("test")
	req := httptest.NewRequest("GET", "/docs", nil)
	recorder := httptest.NewRecorder()
	server.ServeHTTP(recorder, req)

	body := recorder.Body.String()
	primaryStart := strings.Index(body, `<nav class="docs-nav" aria-label="Primary">`)
	if primaryStart < 0 {
		t.Fatal("primary navigation is missing")
	}
	primaryEnd := strings.Index(body[primaryStart:], "</nav>")
	if primaryEnd < 0 {
		t.Fatal("primary navigation is not closed")
	}
	primary := body[primaryStart : primaryStart+primaryEnd]
	for _, href := range []string{`href="/"`, `href="/demo"`, `href="/docs"`} {
		if !strings.Contains(primary, href) {
			t.Errorf("primary navigation does not contain %s", href)
		}
	}
	for _, href := range []string{`href="/docs/getting-started"`, `href="/docs/connections"`, `href="/docs/workspace"`, `href="/docs/ai"`, `href="/docs/plugins"`, `href="/search"`} {
		if strings.Contains(primary, href) {
			t.Errorf("primary navigation unexpectedly contains %s", href)
		}
	}
	if !strings.Contains(body, `<aside class="docs-sidebar" aria-label="Documentation sections">`) {
		t.Fatal("documentation sidebar is missing")
	}
}

func TestSearchAPI(t *testing.T) {
	t.Parallel()

	server := New("test")
	req := httptest.NewRequest("GET", "/api/search?q=MongoDB", nil)
	recorder := httptest.NewRecorder()
	server.ServeHTTP(recorder, req)

	if recorder.Code != 200 {
		t.Fatalf("status = %d, want 200", recorder.Code)
	}
	if got := recorder.Header().Get("Content-Type"); !strings.HasPrefix(got, "application/json") {
		t.Fatalf("content type = %q, want application/json", got)
	}
	var payload struct {
		Query   string `json:"query"`
		Results []struct {
			Path    string `json:"path"`
			Title   string `json:"title"`
			Summary string `json:"summary"`
		} `json:"results"`
	}
	if err := json.Unmarshal(recorder.Body.Bytes(), &payload); err != nil {
		t.Fatalf("response is not valid JSON: %v", err)
	}
	if payload.Query != "MongoDB" {
		t.Errorf("query = %q, want %q", payload.Query, "MongoDB")
	}
	if len(payload.Results) == 0 {
		t.Fatal("results are empty, want at least the connections page")
	}
	if payload.Results[0].Path != "/docs/connections" {
		t.Errorf("top result = %q, want /docs/connections", payload.Results[0].Path)
	}
	if payload.Results[0].Title == "" || payload.Results[0].Summary == "" {
		t.Errorf("result missing title/summary: %+v", payload.Results[0])
	}
}

func TestSearchAPIEmptyQuery(t *testing.T) {
	t.Parallel()

	server := New("test")
	for _, path := range []string{"/api/search", "/api/search?q=", "/api/search?q=%20%20"} {
		t.Run(path, func(t *testing.T) {
			req := httptest.NewRequest("GET", path, nil)
			recorder := httptest.NewRecorder()
			server.ServeHTTP(recorder, req)

			if recorder.Code != 200 {
				t.Fatalf("status = %d, want 200", recorder.Code)
			}
			var payload struct {
				Query   string   `json:"query"`
				Results []string `json:"results"`
			}
			if err := json.Unmarshal(recorder.Body.Bytes(), &payload); err != nil {
				t.Fatalf("response is not valid JSON: %v", err)
			}
			if payload.Query != "" {
				t.Errorf("query = %q, want empty", payload.Query)
			}
			if len(payload.Results) != 0 {
				t.Errorf("results = %v, want none", payload.Results)
			}
		})
	}
}

func TestSearchAPIMatchesBodyContent(t *testing.T) {
	t.Parallel()

	server := New("test")

	// "AES-256-GCM" only occurs in the connections page markdown body —
	// not in any title, lede, or keyword list.
	req := httptest.NewRequest("GET", "/api/search?q=AES-256-GCM", nil)
	recorder := httptest.NewRecorder()
	server.ServeHTTP(recorder, req)

	if recorder.Code != 200 {
		t.Fatalf("status = %d, want 200", recorder.Code)
	}
	var payload struct {
		Results []struct {
			Path string `json:"path"`
		} `json:"results"`
	}
	if err := json.Unmarshal(recorder.Body.Bytes(), &payload); err != nil {
		t.Fatalf("response is not valid JSON: %v", err)
	}
	found := false
	for _, result := range payload.Results {
		if result.Path == "/docs/connections" {
			found = true
		}
	}
	if !found {
		t.Errorf("body-only search hit missing: results do not include /docs/connections")
	}
}

func TestSearchPageRemoved(t *testing.T) {
	t.Parallel()

	server := New("test")
	req := httptest.NewRequest("GET", "/search?q=MongoDB", nil)
	recorder := httptest.NewRecorder()
	server.ServeHTTP(recorder, req)

	if recorder.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want 404 for removed /search page", recorder.Code)
	}
}

func TestDocsPageRendersMarkdown(t *testing.T) {
	t.Parallel()

	server := New("test")
	req := httptest.NewRequest("GET", "/docs/plugins", nil)
	recorder := httptest.NewRecorder()
	server.ServeHTTP(recorder, req)
	body := recorder.Body.String()

	for _, want := range []string{"<h2 id=\"perk-v1\">Perk v1</h2>", `<code class="language-sh">perk-workbench plugin list`} {
		if !strings.Contains(body, want) {
			t.Errorf("rendered docs body missing %q", want)
		}
	}
}

func TestUnknownRoute(t *testing.T) {
	t.Parallel()

	req := httptest.NewRequest("GET", "/unknown", nil)
	recorder := httptest.NewRecorder()
	New("test").ServeHTTP(recorder, req)
	if recorder.Code != 404 {
		t.Fatalf("status = %d, want 404", recorder.Code)
	}
}

func TestPostKnownRoute(t *testing.T) {
	t.Parallel()

	req := httptest.NewRequest("POST", "/", nil)
	recorder := httptest.NewRecorder()
	New("test").ServeHTTP(recorder, req)
	if recorder.Code != 405 {
		t.Fatalf("status = %d, want 405", recorder.Code)
	}
	if got := recorder.Header().Get("Allow"); got != "GET, HEAD" {
		t.Fatalf("Allow = %q, want GET, HEAD", got)
	}
}

func TestHealthz(t *testing.T) {
	t.Parallel()

	req := httptest.NewRequest("GET", "/healthz", nil)
	recorder := httptest.NewRecorder()
	New("test").ServeHTTP(recorder, req)
	if recorder.Code != 200 {
		t.Fatalf("status = %d, want 200", recorder.Code)
	}
	if got := recorder.Body.String(); got != "ok\n" {
		t.Fatalf("body = %q, want %q", got, "ok\\n")
	}
	if got := recorder.Header().Get("Content-Type"); got != "text/plain; charset=utf-8" {
		t.Fatalf("content type = %q, want text/plain; charset=utf-8", got)
	}
}

func TestStaticAssets(t *testing.T) {
	t.Parallel()

	server := New("test")
	manifest := loadAssetManifest()

	// Every entry the templates reference must resolve and be served with
	// immutable caching, because Vite content-hashes the filenames.
	for _, name := range []string{"site", "app", "demo"} {
		entry, err := manifest.entry(name)
		if err != nil {
			t.Fatalf("manifest entry %q: %v", name, err)
		}
		for _, file := range append([]string{entry.File}, entry.CSS...) {
			path := "/static/" + file
			req := httptest.NewRequest("GET", path, nil)
			recorder := httptest.NewRecorder()
			server.ServeHTTP(recorder, req)

			if recorder.Code != 200 {
				t.Fatalf("%s: status = %d, want 200", path, recorder.Code)
			}
			if recorder.Body.Len() == 0 {
				t.Fatalf("%s: empty body", path)
			}
			if got := recorder.Header().Get("Cache-Control"); got != "public, max-age=31536000, immutable" {
				t.Errorf("%s: cache control = %q, want immutable", path, got)
			}
		}
	}
}

func TestPagesUseHashedAssets(t *testing.T) {
	t.Parallel()

	server := New("test")
	manifest := loadAssetManifest()

	stylesheet, err := manifest.entry("site")
	if err != nil {
		t.Fatal(err)
	}
	app, err := manifest.entry("app")
	if err != nil {
		t.Fatal(err)
	}
	demo, err := manifest.entry("demo")
	if err != nil {
		t.Fatal(err)
	}

	tests := []struct {
		path  string
		entry assetEntry
	}{
		{path: "/", entry: app},
		{path: "/demo", entry: demo},
	}
	for _, tt := range tests {
		t.Run(tt.path, func(t *testing.T) {
			req := httptest.NewRequest("GET", tt.path, nil)
			recorder := httptest.NewRecorder()
			server.ServeHTTP(recorder, req)

			body := recorder.Body.String()
			if strings.Contains(body, "?v=") {
				t.Error("page still uses a manual version query string")
			}
			if !strings.Contains(body, "/static/"+stylesheet.File) {
				t.Error("page does not reference the hashed stylesheet")
			}
			if !strings.Contains(body, "/static/"+tt.entry.File) {
				t.Errorf("page does not reference the hashed %q bundle", tt.entry.Name)
			}
			if tt.path == "/demo" {
				for _, css := range demo.CSS {
					if !strings.Contains(body, "/static/"+css) {
						t.Errorf("demo page does not reference its CSS chunk %s", css)
					}
				}
			}
		})
	}
}
