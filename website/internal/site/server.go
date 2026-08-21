package site

import (
	"embed"
	"encoding/json"
	"fmt"
	"html/template"
	"io/fs"
	"net/http"
	"strings"
)

//go:embed all:templates all:assets
var embedded embed.FS

// assetEntry mirrors the fields of a Vite build manifest entry
// (assets/dist/.vite/manifest.json). File paths are relative to the dist
// directory; the server prefixes /static/ when emitting URLs.
type assetEntry struct {
	File string   `json:"file"`
	Name string   `json:"name"`
	CSS  []string `json:"css"`
}

type assetManifest map[string]assetEntry

// loadAssetManifest reads the manifest written by the frontend build. It
// panics on failure so a stale or missing build fails at startup, never at
// request time.
func loadAssetManifest() assetManifest {
	data, err := fs.ReadFile(embedded, "assets/dist/.vite/manifest.json")
	if err != nil {
		panic(fmt.Errorf("read frontend asset manifest: %w", err))
	}
	var manifest assetManifest
	if err := json.Unmarshal(data, &manifest); err != nil {
		panic(fmt.Errorf("parse frontend asset manifest: %w", err))
	}
	return manifest
}

func (m assetManifest) entry(name string) (assetEntry, error) {
	for _, entry := range m {
		if entry.Name == name {
			return entry, nil
		}
	}
	return assetEntry{}, fmt.Errorf("frontend asset %q not found in manifest", name)
}

// assetFuncs exposes the hashed asset URLs to templates. Vite content-hashes
// every build output, so a changed frontend file automatically gets a new URL
// and the /static/ handler can serve those files with immutable caching.
func assetFuncs(m assetManifest) template.FuncMap {
	return template.FuncMap{
		"asset": func(name string) (string, error) {
			entry, err := m.entry(name)
			if err != nil {
				return "", err
			}
			return "/static/" + entry.File, nil
		},
		"cssAssets": func(name string) ([]string, error) {
			entry, err := m.entry(name)
			if err != nil {
				return nil, err
			}
			urls := make([]string, 0, len(entry.CSS))
			for _, css := range entry.CSS {
				urls = append(urls, "/static/"+css)
			}
			return urls, nil
		},
	}
}

type pageData struct {
	Title         string
	Path          string
	Docs          bool
	Query         string
	SearchMessage string
	Results       []Page
	Version       string
}

func New(version string) http.Handler {
	mux := http.NewServeMux()
	pages := PageCatalogue()
	assets := loadAssetManifest()

	route := func(path string, page Page) {
		mux.HandleFunc(path, func(w http.ResponseWriter, r *http.Request) {
			if path == "/" && r.URL.Path != "/" {
				http.NotFound(w, r)
				return
			}
			if !methodAllowed(w, r) {
				return
			}
			renderPage(w, r, version, page, assets)
		})
	}

	for _, page := range pages {
		switch page.Path {
		case "/", "/demo", "/docs", "/docs/getting-started", "/docs/connections", "/docs/workspace", "/docs/ai", "/docs/plugins":
			route(page.Path, page)
		}
	}

	mux.HandleFunc("/ws/tui", func(w http.ResponseWriter, r *http.Request) {
		if !methodAllowed(w, r) {
			return
		}
		newTerminalServer().ServeHTTP(w, r)
	})

	mux.HandleFunc("/search", func(w http.ResponseWriter, r *http.Request) {
		if !methodAllowed(w, r) {
			return
		}
		renderSearch(w, r, version, pages, assets)
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

	// Built bundles (dist) are served under /static/assets/ with a content
	// hash in the filename: safe to cache forever. Everything else under
	// /static/ (fonts, images) must be revalidated.
	buildFiles, err := fs.Sub(embedded, "assets/dist")
	if err != nil {
		panic(err)
	}
	buildServer := http.StripPrefix("/static/", http.FileServer(http.FS(buildFiles)))
	mux.HandleFunc("/static/assets/", func(w http.ResponseWriter, r *http.Request) {
		if !methodAllowed(w, r) {
			return
		}
		w.Header().Set("Cache-Control", "public, max-age=31536000, immutable")
		buildServer.ServeHTTP(w, r)
	})

	static, err := fs.Sub(embedded, "assets")
	if err != nil {
		panic(err)
	}
	fileServer := http.StripPrefix("/static/", http.FileServer(http.FS(static)))
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
func renderPage(w http.ResponseWriter, r *http.Request, version string, page Page, assets assetManifest) {
	t := template.Must(template.New("base").Funcs(assetFuncs(assets)).ParseFS(embedded,
		"templates/base.html",
		"templates/partials/navigation.html",
		"templates/partials/docs-sidebar.html",
		"templates/partials/search-results.html",
		"templates/"+page.Template,
	))
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	if r.Method == http.MethodHead {
		return
	}
	if err := t.ExecuteTemplate(w, "base", pageData{Title: page.Title, Path: page.Path, Docs: strings.HasPrefix(page.Path, "/docs"), Version: version}); err != nil {
		return
	}
}

func renderSearch(w http.ResponseWriter, r *http.Request, version string, pages []Page, assets assetManifest) {
	q := strings.TrimSpace(r.URL.Query().Get("q"))
	data := pageData{
		Title:   "Search",
		Path:    "/search",
		Docs:    true,
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
	t := template.Must(template.New("base").Funcs(assetFuncs(assets)).ParseFS(embedded,
		"templates/base.html",
		"templates/partials/navigation.html",
		"templates/partials/docs-sidebar.html",
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
