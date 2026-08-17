package site

import (
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
		{path: "/docs/getting-started", title: "Getting Started"},
		{path: "/docs/connections", title: "Connections"},
		{path: "/docs/workspace", title: "Workspace"},
		{path: "/docs/ai", title: "AI"},
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

func TestSearchPage(t *testing.T) {
	t.Parallel()

	server := New("test")
	req := httptest.NewRequest("GET", "/search?q=MongoDB", nil)
	recorder := httptest.NewRecorder()
	server.ServeHTTP(recorder, req)

	if recorder.Code != 200 {
		t.Fatalf("status = %d, want 200", recorder.Code)
	}
	if got := recorder.Header().Get("Content-Type"); !strings.HasPrefix(got, "text/html") {
		t.Fatalf("content type = %q, want text/html", got)
	}
	body := recorder.Body.String()
	if !strings.Contains(body, "MongoDB") {
		t.Errorf("body does not contain search query")
	}
	if !strings.Contains(body, "<html") || !strings.Contains(body, "<title>") {
		t.Errorf("normal search response is not a complete HTML page")
	}
}

func TestSearchHXRequest(t *testing.T) {
	t.Parallel()

	server := New("test")
	req := httptest.NewRequest("GET", "/search?q=MongoDB", nil)
	req.Header.Set("HX-Request", "true")
	recorder := httptest.NewRecorder()
	server.ServeHTTP(recorder, req)

	if recorder.Code != 200 {
		t.Fatalf("status = %d, want 200", recorder.Code)
	}
	body := recorder.Body.String()
	if !strings.Contains(body, "MongoDB") {
		t.Errorf("body does not contain search query")
	}
	if strings.Contains(body, "<html") || strings.Contains(body, "<title>") {
		t.Errorf("HX response unexpectedly contains the full HTML shell")
	}
}

func TestSearchEmptyQuery(t *testing.T) {
	t.Parallel()

	server := New("test")
	for _, path := range []string{"/search", "/search?q="} {
		t.Run(path, func(t *testing.T) {
			req := httptest.NewRequest("GET", path, nil)
			recorder := httptest.NewRecorder()
			server.ServeHTTP(recorder, req)

			if recorder.Code != 200 {
				t.Fatalf("status = %d, want 200", recorder.Code)
			}
			if !strings.Contains(recorder.Body.String(), "Enter a search term") {
				t.Errorf("body does not contain exact empty-query message")
			}
		})
	}
}

func TestSearchEscapesQuery(t *testing.T) {
	t.Parallel()

	server := New("test")
	req := httptest.NewRequest("GET", "/search?q=%3Cscript%3E", nil)
	recorder := httptest.NewRecorder()
	server.ServeHTTP(recorder, req)

	if recorder.Code != 200 {
		t.Fatalf("status = %d, want 200", recorder.Code)
	}
	body := recorder.Body.String()
	if !strings.Contains(body, "&lt;script&gt;") {
		t.Errorf("body does not HTML-escape the query")
	}
	if strings.Contains(body, "<script>") {
		t.Errorf("body contains an unescaped script element")
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

func TestStaticCSS(t *testing.T) {
	t.Parallel()

	req := httptest.NewRequest("GET", "/static/site.css", nil)
	recorder := httptest.NewRecorder()
	New("test").ServeHTTP(recorder, req)
	if recorder.Code != 200 {
		t.Fatalf("status = %d, want 200", recorder.Code)
	}
	if got := recorder.Header().Get("Content-Type"); !strings.HasPrefix(got, "text/css") {
		t.Fatalf("content type = %q, want text/css", got)
	}
	if recorder.Body.Len() == 0 {
		t.Fatal("CSS response is empty")
	}
	if got := recorder.Header().Get("Cache-Control"); got != "no-cache" {
		t.Fatalf("cache control = %q, want no-cache", got)
	}
}
