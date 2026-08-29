package site

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"regexp"
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
	primaryPattern := regexp.MustCompile(`(?s)<nav\b[^>]*\baria-label="Primary"[^>]*>(.*?)</nav>`)
	primaryMatches := primaryPattern.FindAllStringSubmatch(body, -1)
	if len(primaryMatches) != 1 {
		t.Fatalf("page contains %d Primary navigation landmarks, want exactly 1", len(primaryMatches))
	}
	primary := primaryMatches[0][1]
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
	inputPattern := regexp.MustCompile(`<input\b[^>]*>`)
	var searchInput string
	for _, input := range inputPattern.FindAllString(primary, -1) {
		if strings.Contains(input, `id="docs-nav-search"`) {
			searchInput = input
			break
		}
	}
	if searchInput == "" {
		t.Fatal("primary navigation is missing the docs search trigger")
	}
	for _, marker := range []string{
		`type="search"`,
		`placeholder="Search docs…"`,
		`aria-label="Search docs"`,
		`readonly`,
		`data-search-open`,
		`aria-haspopup="dialog"`,
	} {
		if !strings.Contains(searchInput, marker) {
			t.Errorf("docs search trigger does not expose %s", marker)
		}
	}
	if strings.Contains(primary, `<button`) {
		t.Error("primary navigation unexpectedly contains a button search trigger")
	}

	headerStart := strings.Index(body, "<header")
	if headerStart < 0 {
		t.Fatal("site header is missing")
	}
	headerEnd := strings.Index(body[headerStart:], "</header>")
	if headerEnd < 0 {
		t.Fatal("site header is not closed")
	}
	header := body[headerStart : headerStart+headerEnd]
	buttonPattern := regexp.MustCompile(`<button\b[^>]*>`)
	var searchButtons []string
	for _, button := range buttonPattern.FindAllString(header, -1) {
		if strings.Contains(button, `data-search-open`) {
			searchButtons = append(searchButtons, button)
		}
	}
	if len(searchButtons) != 1 {
		t.Fatalf("header contains %d mobile docs search buttons, want exactly 1", len(searchButtons))
	}
	for _, marker := range []string{
		`type="button"`,
		`data-search-open`,
		`aria-label="Search docs"`,
		`aria-haspopup="dialog"`,
	} {
		if !strings.Contains(searchButtons[0], marker) {
			t.Errorf("mobile docs search button does not expose %s", marker)
		}
	}
	if !strings.Contains(header, `id="theme-toggle"`) {
		t.Error("header does not contain the theme toggle")
	}
	if strings.Contains(primary, `contenteditable="true"`) || strings.Contains(primary, `aria-autocomplete=`) {
		t.Error("topbar search input must not expose editable search behavior")
	}

	sidebarOpen := regexp.MustCompile(`<aside\b[^>]*\baria-label="Documentation sections"[^>]*>`)
	sidebarMatch := sidebarOpen.FindStringIndex(body)
	if sidebarMatch == nil {
		t.Fatal("documentation sidebar is missing")
	}
	sidebarStart := sidebarMatch[0]
	sidebarEnd := strings.Index(body[sidebarStart:], "</aside>")
	if sidebarEnd < 0 {
		t.Fatal("documentation sidebar is not closed")
	}
	sidebar := body[sidebarStart : sidebarStart+sidebarEnd]

	// Match links structurally so indentation or line wrapping does not affect
	// the navigation contract.
	disclosurePattern := regexp.MustCompile(`(?s)<details\b[^>]*\bopen\b[^>]*>(.*?)</details>`)
	disclosureMatches := disclosurePattern.FindAllStringSubmatch(sidebar, -1)
	if len(disclosureMatches) != 1 {
		t.Fatalf("sidebar contains %d mobile disclosures, want exactly 1", len(disclosureMatches))
	}
	mobileDisclosure := disclosureMatches[0][0]
	summaryPattern := regexp.MustCompile(`(?s)<summary\b[^>]*>\s*Documentation\s*</summary>`)
	if count := len(summaryPattern.FindAllString(mobileDisclosure, -1)); count != 1 {
		t.Errorf("mobile disclosure contains %d Documentation summaries, want exactly 1", count)
	}

	desktopStart := strings.Index(sidebar, "</details>")
	if desktopStart < 0 {
		t.Fatal("documentation sidebar is missing its desktop section")
	}
	desktop := sidebar[desktopStart:]
	navPattern := regexp.MustCompile(`(?s)<nav\b[^>]*>(.*?)</nav>`)
	desktopNavMatches := navPattern.FindAllStringSubmatch(desktop, -1)
	if len(desktopNavMatches) != 1 {
		t.Fatalf("desktop documentation section contains %d nav elements, want exactly 1", len(desktopNavMatches))
	}
	if count := len(regexp.MustCompile(`(?s)<p\b[^>]*>\s*Documentation\s*</p>`).FindAllString(sidebar, -1)); count != 1 {
		t.Errorf("sidebar contains %d desktop Documentation labels, want exactly 1", count)
	}

	extractNav := func(scope, wrapper string) string {
		t.Helper()
		matches := navPattern.FindAllStringSubmatch(scope, -1)
		if len(matches) != 1 {
			t.Fatalf("%s wrapper contains %d nav elements, want exactly 1", wrapper, len(matches))
		}
		return matches[0][1]
	}
	mobileNav := extractNav(mobileDisclosure, "mobile")
	desktopNav := extractNav(desktop, "desktop")

	linkPattern := func(path, title string) *regexp.Regexp {
		return regexp.MustCompile(fmt.Sprintf(
			`(?s)<a\b[^>]*\shref="%s"[^>]*>\s*%s\s*</a>`,
			regexp.QuoteMeta(path),
			regexp.QuoteMeta(title),
		))
	}
	assertCatalogue := func(wrapper, nav string) {
		t.Helper()
		overviewMatches := linkPattern("/docs", "Overview").FindAllString(nav, -1)
		if len(overviewMatches) != 1 {
			t.Errorf("%s sidebar contains %d Overview links, want exactly 1", wrapper, len(overviewMatches))
		} else if !strings.Contains(overviewMatches[0], `aria-current="page"`) {
			t.Errorf("%s Overview link is missing aria-current page marker on /docs", wrapper)
		}
		for _, doc := range docPages(LoadPages()) {
			if matches := linkPattern(doc.Path, doc.Title).FindAllString(nav, -1); len(matches) != 1 {
				t.Errorf("%s sidebar contains %q %d times, want exactly 1", wrapper, doc.Title, len(matches))
			}
		}
	}
	assertCatalogue("mobile", mobileNav)
	assertCatalogue("desktop", desktopNav)
	if count := strings.Count(mobileDisclosure, "</nav>"); count != 1 {
		t.Errorf("mobile disclosure contains %d nav closing tags, want exactly 1", count)
	}
	if count := strings.Count(desktop, "</nav>"); count != 1 {
		t.Errorf("desktop wrapper contains %d nav closing tags, want exactly 1", count)
	}
	if count := strings.Count(sidebar, "</details>"); count != 1 {
		t.Errorf("sidebar contains %d details closing tags, want exactly 1", count)
	}
	if count := len(regexp.MustCompile(`<div\b[^>]*>`).FindAllString(sidebar, -1)); count != 1 {
		t.Errorf("sidebar contains %d desktop wrappers, want exactly 1", count)
	}
	if count := len(regexp.MustCompile(`<nav\b[^>]*>`).FindAllString(sidebar, -1)); count != 2 {
		t.Errorf("sidebar contains %d docs nav elements, want exactly 2", count)
	}
	if count := len(regexp.MustCompile(`<summary\b[^>]*>`).FindAllString(sidebar, -1)); count != 1 {
		t.Errorf("sidebar contains %d summary controls, want exactly 1 mobile summary", count)
	}
	if count := len(regexp.MustCompile(`<details\b[^>]*>`).FindAllString(sidebar, -1)); count != 1 {
		t.Errorf("sidebar contains %d details elements, want exactly 1 mobile disclosure", count)
	}
	if count := len(regexp.MustCompile(`(?s)<p\b[^>]*>\s*Documentation\s*</p>`).FindAllString(sidebar, -1)); count != 1 {
		t.Errorf("sidebar contains %d desktop Documentation labels, want exactly 1", count)
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
	footerNavPattern := regexp.MustCompile(`(?s)<nav\b[^>]*\baria-label="Document navigation"[^>]*>(.*?)</nav>`)
	footerNav := func(body string) string {
		t.Helper()
		matches := footerNavPattern.FindAllStringSubmatch(body, -1)
		if len(matches) > 1 {
			t.Fatalf("body contains %d Document navigation landmarks, want at most 1", len(matches))
		}
		if len(matches) == 0 {
			return ""
		}
		return matches[0][1]
	}
	assertLink := func(body string, direction string, doc Page) {
		t.Helper()
		nav := footerNav(body)
		if nav == "" {
			t.Fatal("document-footer navigation landmark is missing")
		}
		linkPattern := regexp.MustCompile(fmt.Sprintf(
			`(?s)<a\b[^>]*\shref="%s"[^>]*>.*?</a>`,
			regexp.QuoteMeta(doc.Path),
		))
		matches := linkPattern.FindAllString(nav, -1)
		if len(matches) != 1 {
			t.Fatalf("%s navigation does not contain exactly one link to %s (found %d)", direction, doc.Path, len(matches))
		}
		link := matches[0]
		labelPattern := regexp.MustCompile(`(?s)<span\b[^>]*>\s*` + regexp.QuoteMeta(direction) + `\s*</span>`)
		if !labelPattern.MatchString(link) {
			t.Errorf("%s link to %s is missing its visible label", direction, doc.Path)
		}
		titlePattern := regexp.MustCompile(`(?s)<strong\b[^>]*>\s*` + regexp.QuoteMeta(doc.Title) + `\s*</strong>`)
		if !titlePattern.MatchString(link) {
			t.Errorf("%s link does not label its destination %q", direction, doc.Title)
		}
	}
	hasDirection := func(nav, direction string) bool {
		return regexp.MustCompile(`(?s)<span\b[^>]*>\s*` + regexp.QuoteMeta(direction) + `\s*</span>`).MatchString(nav)
	}

	t.Run("middle document links both directions", func(t *testing.T) {
		body := get(docs[1].Path)
		if footerNav(body) == "" {
			t.Fatal("document-footer navigation landmark is missing")
		}
		assertLink(body, "Previous", docs[0])
		assertLink(body, "Next", docs[2])
	})
	t.Run("first document links forward only", func(t *testing.T) {
		body := get(docs[0].Path)
		assertLink(body, "Next", docs[1])
		if hasDirection(footerNav(body), "Previous") {
			t.Errorf("first document %s exposes a Previous link", docs[0].Path)
		}
	})
	t.Run("last document links backward only", func(t *testing.T) {
		last := docs[len(docs)-1]
		body := get(last.Path)
		assertLink(body, "Previous", docs[len(docs)-2])
		if hasDirection(footerNav(body), "Next") {
			t.Errorf("last document %s exposes a Next link", last.Path)
		}
	})
	t.Run("overview has no footer", func(t *testing.T) {
		body := get("/docs")
		if footerNav(body) != "" {
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
func TestSearchFragment(t *testing.T) {
	t.Parallel()

	server := New("test")
	t.Run("matches", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/search/fragment?q=MongoDB", nil)
		recorder := httptest.NewRecorder()
		server.ServeHTTP(recorder, req)

		if recorder.Code != http.StatusOK {
			t.Fatalf("status = %d, want 200", recorder.Code)
		}
		if got := recorder.Header().Get("Content-Type"); !strings.HasPrefix(got, "text/html") {
			t.Fatalf("content type = %q, want text/html", got)
		}
		body := recorder.Body.String()
		if got := strings.Count(body, `id="spotlight-output"`); got != 1 {
			t.Fatalf("spotlight output wrappers = %d, want 1", got)
		}
		if !strings.Contains(body, `href="/docs/connections"`) {
			t.Fatal("connections result link missing")
		}
		if json.Valid(recorder.Body.Bytes()) {
			t.Fatal("fragment response is valid JSON")
		}
	})

	t.Run("blank query", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/search/fragment?q=%20%20", nil)
		recorder := httptest.NewRecorder()
		server.ServeHTTP(recorder, req)

		if recorder.Code != http.StatusOK {
			t.Fatalf("status = %d, want 200", recorder.Code)
		}
		body := recorder.Body.String()
		if got := strings.Count(body, `id="spotlight-output"`); got != 1 {
			t.Fatalf("spotlight output wrappers = %d, want 1", got)
		}
		if strings.Contains(body, "No results found") {
			t.Fatal("blank query rendered no-results text")
		}
	})

	t.Run("no match", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/search/fragment?q=__not_a_real_search_term__", nil)
		recorder := httptest.NewRecorder()
		server.ServeHTTP(recorder, req)

		if recorder.Code != http.StatusOK {
			t.Fatalf("status = %d, want 200", recorder.Code)
		}
		body := recorder.Body.String()
		if got := strings.Count(body, `id="spotlight-output"`); got != 1 {
			t.Fatalf("spotlight output wrappers = %d, want 1", got)
		}
		if !strings.Contains(body, "No results found") {
			t.Fatal("no-match query omitted no-results text")
		}
	})

	t.Run("head", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodHead, "/search/fragment?q=MongoDB", nil)
		recorder := httptest.NewRecorder()
		server.ServeHTTP(recorder, req)

		if recorder.Code != http.StatusOK {
			t.Fatalf("status = %d, want 200", recorder.Code)
		}
		if got := recorder.Header().Get("Content-Type"); !strings.HasPrefix(got, "text/html") {
			t.Fatalf("content type = %q, want text/html", got)
		}
		if recorder.Body.Len() != 0 {
			t.Fatalf("body length = %d, want 0", recorder.Body.Len())
		}
	})
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
	for _, name := range []string{"site", "app", "demo", "theme"} {
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

func TestThemeControl_exposesSystemPreference(t *testing.T) {
	t.Parallel()

	recorder := httptest.NewRecorder()
	New("test").ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, "/", nil))
	body := recorder.Body.String()

	buttonPattern := regexp.MustCompile(`<button\b[^>]*\bid="theme-toggle"[^>]*>`)
	buttonMatch := buttonPattern.FindStringIndex(body)
	if buttonMatch == nil {
		t.Fatal("theme control is missing")
	}
	start := buttonMatch[0]
	end := strings.Index(body[start:], "</button>")
	if end < 0 {
		t.Fatal("theme control is not closed")
	}
	control := body[start : start+end]
	for _, marker := range []string{
		`data-theme-preference="dark"`,
		`aria-label="Color theme: dark (activate to use light)"`,
		`title="Color theme: dark (activate to use light)"`,
		`data-theme-icon="sun"`,
		`data-theme-icon="moon"`,
		`data-theme-icon="system"`,
	} {
		if strings.Count(control, marker) != 1 {
			t.Errorf("theme control exposes %q %d times, want exactly once", marker, strings.Count(control, marker))
		}
	}
}

func TestThemeAssetsSupportSystemPreference(t *testing.T) {
	t.Parallel()

	server := New("test")
	manifest := loadAssetManifest()
	assetBody := func(t *testing.T, name string) string {
		t.Helper()
		entry, err := manifest.entry(name)
		if err != nil {
			t.Fatalf("manifest entry %q: %v", name, err)
		}
		recorder := httptest.NewRecorder()
		server.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, "/static/"+entry.File, nil))
		if recorder.Code != http.StatusOK {
			t.Fatalf("%s asset status = %d, want 200", name, recorder.Code)
		}
		return recorder.Body.String()
	}

	bootstrap := assetBody(t, "theme")
	for _, marker := range []string{
		"system",
		"prefers-color-scheme: light",
		"matchMedia",
		"addEventListener",
	} {
		if !strings.Contains(bootstrap, marker) {
			t.Errorf("theme bootstrap does not contain %q", marker)
		}
	}

	runtime := assetBody(t, "app")
	for _, marker := range []string{
		"system",
		"themechange",
		"localStorage",
		"themePreference",
	} {
		if !strings.Contains(runtime, marker) {
			t.Errorf("theme runtime does not contain %q", marker)
		}
	}

	demo := assetBody(t, "demo")
	for _, marker := range []string{
		"dataset.theme",
		`/ws/tui?theme=`,
		"themechange",
	} {
		if !strings.Contains(demo, marker) {
			t.Errorf("terminal demo does not contain %q", marker)
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
