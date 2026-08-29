package site

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// docPages returns the markdown-backed documentation pages in catalogue
// order, so tests track whatever documents LoadPages actually publishes.
func docPages(pages []Page) []Page {
	docs := make([]Page, 0, len(pages))
	for _, page := range pages {
		if strings.HasPrefix(page.Path, "/docs/") {
			docs = append(docs, page)
		}
	}
	return docs
}

func TestRoutes(t *testing.T) {
	t.Parallel()

	server := New("test")
	tests := []struct {
		path  string
		title string
	}{
		{path: "/", title: "Perk Workbench"},
		{path: "/demo", title: "Live demo"},
		{path: "/docs", title: "Documentation"},
	}
	for _, doc := range docPages(LoadPages()) {
		tests = append(tests, struct {
			path  string
			title string
		}{doc.Path, doc.Title})
	}
	assertTestRoutes(t, server, tests)
}

func assertTestRoutes(t *testing.T, server http.Handler, tests []struct {
	path  string
	title string
}) {
	t.Helper()
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
	if !strings.Contains(primary, `<input class="docs-nav-search" type="search"`) ||
		!strings.Contains(primary, `placeholder="Search docs…"`) ||
		!strings.Contains(primary, `readonly`) ||
		!strings.Contains(primary, `data-search-open`) ||
		!strings.Contains(primary, `aria-haspopup="dialog"`) ||
		!strings.Contains(primary, `<svg class="docs-nav-search-icon size-4" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" aria-hidden="true">`) ||
		!strings.Contains(primary, `<kbd class="docs-nav-search-shortcut" aria-hidden="true">⌘K</kbd>`) {
		t.Error("primary navigation does not contain the readonly docs search trigger, icon, and shortcut hint")
	}
	if strings.Contains(primary, `<button class="docs-nav-search-button"`) {
		t.Error("primary navigation unexpectedly contains the mobile docs search button")
	}

	actionsStart := strings.Index(body, `<div class="site-header-actions">`)
	if actionsStart < 0 {
		t.Fatal("header actions are missing")
	}
	actionsEnd := strings.Index(body[actionsStart:], "</div>")
	if actionsEnd < 0 {
		t.Fatal("header actions are not closed")
	}
	actions := body[actionsStart : actionsStart+actionsEnd]
	if count := strings.Count(actions, `<button class="docs-nav-search-button"`); count != 1 {
		t.Errorf("header actions contain %d mobile docs search buttons, want exactly 1", count)
	}
	if !strings.Contains(actions, `<button class="docs-nav-search-button" type="button" data-search-open aria-label="Search docs" aria-haspopup="dialog">`) ||
		!strings.Contains(actions, `<svg class="docs-nav-search-button-icon size-4" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" aria-hidden="true">`) ||
		!strings.Contains(actions, `id="theme-toggle"`) {
		t.Error("header actions do not contain the mobile search button and theme toggle")
	}
	if strings.Contains(primary, `contenteditable="true"`) || strings.Contains(primary, `aria-autocomplete=`) {
		t.Error("topbar search input must not expose editable search behavior")
	}

	sidebarStart := strings.Index(body, `<aside class="docs-sidebar" aria-label="Documentation sections">`)
	if sidebarStart < 0 {
		t.Fatal("documentation sidebar is missing")
	}
	sidebarEnd := strings.Index(body[sidebarStart:], "</aside>")
	if sidebarEnd < 0 {
		t.Fatal("documentation sidebar is not closed")
	}
	sidebar := body[sidebarStart : sidebarStart+sidebarEnd]
	if strings.Contains(sidebar, "Search docs") || strings.Contains(sidebar, "docs-sidebar-search") {
		t.Error("documentation sidebar unexpectedly contains the docs search control")
	}

	// The Overview anchor gains aria-current on /docs itself, so match on
	// the href and the visible label separately.
	if count := strings.Count(sidebar, `href="/docs"`); count != 1 {
		t.Errorf("sidebar contains %d Overview hrefs, want exactly 1", count)
	}
	if count := strings.Count(sidebar, ">Overview</a>"); count != 1 {
		t.Errorf("sidebar contains %d Overview labels, want exactly 1", count)
	}
	for _, doc := range docPages(LoadPages()) {
		link := fmt.Sprintf(`href="%s">%s</a>`, doc.Path, doc.Title)
		if count := strings.Count(sidebar, link); count != 1 {
			t.Errorf("sidebar contains %q %d times, want exactly 1", link, count)
		}
	}
}

func TestDocPreviousNext(t *testing.T) {
	t.Parallel()

	docs := docPages(LoadPages())
	if len(docs) < 3 {
		t.Fatalf("doc catalogue has %d documents, want at least 3", len(docs))
	}

	server := New("test")
	get := func(path string) string {
		t.Helper()
		req := httptest.NewRequest("GET", path, nil)
		recorder := httptest.NewRecorder()
		server.ServeHTTP(recorder, req)
		if recorder.Code != 200 {
			t.Fatalf("%s: status = %d, want 200", path, recorder.Code)
		}
		return recorder.Body.String()
	}
	assertPrev := func(body string, doc Page) {
		t.Helper()
		if !strings.Contains(body, fmt.Sprintf(`<a class="doc-nav-card doc-nav-prev" href="%s">`, doc.Path)) {
			t.Errorf("body does not link Previous to %s", doc.Path)
		}
		if !strings.Contains(body, "<strong>"+doc.Title+"</strong>") {
			t.Errorf("Previous link does not label its destination %q", doc.Title)
		}
	}
	assertNext := func(body string, doc Page) {
		t.Helper()
		if !strings.Contains(body, fmt.Sprintf(`<a class="doc-nav-card doc-nav-next" href="%s">`, doc.Path)) {
			t.Errorf("body does not link Next to %s", doc.Path)
		}
		if !strings.Contains(body, "<strong>"+doc.Title+"</strong>") {
			t.Errorf("Next link does not label its destination %q", doc.Title)
		}
	}

	t.Run("middle document links both directions", func(t *testing.T) {
		body := get(docs[1].Path)
		if !strings.Contains(body, `<nav class="doc-footer-nav" aria-label="Document navigation">`) {
			t.Fatal("document-footer navigation landmark is missing")
		}
		assertPrev(body, docs[0])
		assertNext(body, docs[2])
	})
	t.Run("first document links forward only", func(t *testing.T) {
		body := get(docs[0].Path)
		assertNext(body, docs[1])
		if strings.Contains(body, "doc-nav-prev") {
			t.Errorf("first document %s exposes a Previous link", docs[0].Path)
		}
	})
	t.Run("last document links backward only", func(t *testing.T) {
		last := docs[len(docs)-1]
		body := get(last.Path)
		assertPrev(body, docs[len(docs)-2])
		if strings.Contains(body, "doc-nav-next") {
			t.Errorf("last document %s exposes a Next link", last.Path)
		}
	})
	t.Run("overview has no footer", func(t *testing.T) {
		body := get("/docs")
		if strings.Contains(body, "doc-footer-nav") {
			t.Error("overview renders a document footer")
		}
	})
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

func TestSearchAIOpenAICompatible(t *testing.T) {
	t.Parallel()

	server := New("test")
	req := httptest.NewRequest("GET", "/api/search?q=openai-compatible", nil)
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
		if result.Path == "/docs/ai" {
			found = true
		}
	}
	if !found {
		t.Errorf("search for openai-compatible does not include /docs/ai")
	}
}

func TestAIPageFacts(t *testing.T) {
	t.Parallel()

	req := httptest.NewRequest("GET", "/docs/ai", nil)
	recorder := httptest.NewRecorder()
	New("test").ServeHTTP(recorder, req)
	body := recorder.Body.String()

	for _, want := range []string{
		"$XDG_CONFIG_HOME/perk-workbench/ai.json",
		".perk-workbench/ai.json",
		"Ctrl</kbd>+<kbd>G</kbd>",
		"does not open or toggle AI",
	} {
		if !strings.Contains(body, want) {
			t.Errorf("AI page missing %q", want)
		}
	}
	if strings.Contains(body, "to open the AI assistance flow") {
		t.Error("AI page still claims Ctrl+A opens AI")
	}
}

func TestPluginsPageApprovalForm(t *testing.T) {
	t.Parallel()

	req := httptest.NewRequest("GET", "/docs/plugins", nil)
	recorder := httptest.NewRecorder()
	New("test").ServeHTTP(recorder, req)
	body := recorder.Body.String()

	for _, want := range []string{
		"plugin add --approve SHA256 EXECUTABLE",
		"trusted executable code",
	} {
		if !strings.Contains(body, want) {
			t.Errorf("plugins page missing %q", want)
		}
	}
}

func TestWorkspacePageFacts(t *testing.T) {
	t.Parallel()

	req := httptest.NewRequest("GET", "/docs/workspace", nil)
	recorder := httptest.NewRecorder()
	New("test").ServeHTTP(recorder, req)
	body := recorder.Body.String()

	if !strings.Contains(body, "Ctrl</kbd>+<kbd>Space</kbd>") {
		t.Error("workspace page does not document the Ctrl+Space completion key")
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
