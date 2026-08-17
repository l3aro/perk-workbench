package site

import (
	"embed"
	"html/template"
	"io/fs"
	"net/http"
	"sort"
	"strings"
)

//go:embed templates assets
var embedded embed.FS

type pageData struct {
	Title        string
	Path         string
	Query        string
	SearchMessage string
	Results      []Page
	Version      string
}

func New(version string) http.Handler {
	mux := http.NewServeMux()
	pages := PageCatalogue()

	route := func(path string, page Page) {
		mux.HandleFunc(path, func(w http.ResponseWriter, r *http.Request) {
			if path == "/" && r.URL.Path != "/" {
				http.NotFound(w, r)
				return
			}
			if !methodAllowed(w, r) {
				return
			}
			renderPage(w, r, version, page)
		})
	}

	for _, page := range pages {
		switch page.Path {
		case "/", "/docs/getting-started", "/docs/connections", "/docs/workspace", "/docs/ai", "/docs/plugins":
			route(page.Path, page)
		}
	}

	mux.HandleFunc("/search", func(w http.ResponseWriter, r *http.Request) {
		if !methodAllowed(w, r) {
			return
		}
		renderSearch(w, r, version, pages)
	})
	mux.HandleFunc("/healthz", func(w http.ResponseWriter, r *http.Request) {
		if !methodAllowed(w, r) {
			return
		}
		w.Header().Set("Cache-Control", "no-cache")
		w.Header().Set("Content-Type", "text/plain; charset=utf-8")
		if r.Method == http.MethodGet {
			_, _ = w.Write([]byte("ok\n"))
		}
	})

	static, err := fs.Sub(embedded, "assets")
	if err != nil {
		panic(err)
	}
	fileServer := http.FileServer(http.FS(static))
	mux.HandleFunc("/static/", func(w http.ResponseWriter, r *http.Request) {
		if !methodAllowed(w, r) {
			return
		}
		w.Header().Set("Cache-Control", "no-cache")
		fileServer.ServeHTTP(w, r)
	})

	return mux
}

func methodAllowed(w http.ResponseWriter, r *http.Request) bool {
	if r.Method == http.MethodGet || r.Method == http.MethodHead {
		return true
	}
	w.Header().Set("Allow", "GET, HEAD")
	http.Error(w, http.StatusText(http.StatusMethodNotAllowed), http.StatusMethodNotAllowed)
	return false
}

func renderPage(w http.ResponseWriter, r *http.Request, version string, page Page) {
	t := template.Must(template.ParseFS(embedded,
		"templates/base.html",
		"templates/partials/navigation.html",
		"templates/partials/search-results.html",
		"templates/"+page.Template,
	))
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	if r.Method == http.MethodHead {
		return
	}
	if err := t.ExecuteTemplate(w, "base", pageData{Title: page.Title, Path: page.Path, Version: version}); err != nil {
		return
	}
}

func renderSearch(w http.ResponseWriter, r *http.Request, version string, pages []Page) {
	q := strings.TrimSpace(r.URL.Query().Get("q"))
	data := pageData{
		Title:   "Search",
		Path:    "/search",
		Query:   q,
		Version: version,
	}
	if q == "" {
		data.SearchMessage = "Enter a search term"
	} else {
		data.Results = searchPages(q, pages)
		if len(data.Results) == 0 {
			data.SearchMessage = "No results found"
		}
	}

	t := template.Must(template.ParseFS(embedded,
		"templates/base.html",
		"templates/partials/navigation.html",
		"templates/partials/search-results.html",
		"templates/pages/search.html",
	))
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	if r.Method == http.MethodHead {
		return
	}
	if r.Header.Get("HX-Request") == "true" {
		if err := t.ExecuteTemplate(w, "search-results", data); err != nil {
			return
		}
		return
	}
	if err := t.ExecuteTemplate(w, "base", data); err != nil {
		return
	}
}

func searchPages(query string, pages []Page) []Page {
	needle := strings.ToLower(query)
	results := make([]Page, 0)
	for _, page := range pages {
		if strings.Contains(strings.ToLower(page.Title), needle) ||
			strings.Contains(strings.ToLower(page.Summary), needle) ||
			containsKeyword(page.Keywords, needle) {
			results = append(results, page)
		}
	}
	return results
}

func containsKeyword(keywords []string, needle string) bool {
	for _, keyword := range keywords {
		if strings.Contains(strings.ToLower(keyword), needle) {
			return true
		}
	}
	return false
}

var _ = sort.Strings
