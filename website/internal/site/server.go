package site

import (
	"embed"
	"encoding/json"
	"fmt"
	"html/template"
	"io/fs"
	"net/http"
	"sort"
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
	Title   string
	Path    string
	Docs    bool
	Eyebrow string
	Lede    string
	Body    template.HTML
	Version string

	// DocLinks is the ordered documentation catalogue shared by the docs
	// overview, the sidebar, and search. Previous/Next point at the adjacent
	// documents for a rendered doc page; the overview has neither.
	DocLinks []docLink
	Previous *docLink
	Next     *docLink
}

// docLink is one navigation entry in the documentation catalogue. It carries
// exactly the fields the overview cards and sidebar links render, all of
// which come from the markdown frontmatter.
type docLink struct {
	Path    string
	Title   string
	Summary string
}

// docLinksFromPages derives the ordered documentation catalogue from the
// markdown-backed pages. Only /docs/<name> routes participate: the overview
// is a hand-written page and never appears in the list.
func docLinksFromPages(pages []Page) []docLink {
	links := make([]docLink, 0, len(pages))
	for _, page := range pages {
		if !strings.HasPrefix(page.Path, "/docs/") {
			continue
		}
		links = append(links, docLink{Path: page.Path, Title: page.Title, Summary: page.Summary})
	}
	return links
}

func New(version string) http.Handler {
	mux := http.NewServeMux()
	pages := LoadPages()
	assets := loadAssetManifest()
	docLinks := docLinksFromPages(pages)

	route := func(path string, page Page) {
		mux.HandleFunc(path, func(w http.ResponseWriter, r *http.Request) {
			if path == "/" && r.URL.Path != "/" {
				http.NotFound(w, r)
				return
			}
			if !methodAllowed(w, r) {
				return
			}
			renderPage(w, r, version, page, assets, docLinks)
		})
	}

	// Every page the catalogue produces is registered as a route. The
	// documentation set is content-driven: adding a markdown document makes
	// it reachable and searchable without touching this list.
	for _, page := range pages {
		route(page.Path, page)
	}

	mux.HandleFunc("/ws/tui", func(w http.ResponseWriter, r *http.Request) {
		if !methodAllowed(w, r) {
			return
		}
		newTerminalServer().ServeHTTP(w, r)
	})

	mux.HandleFunc("/api/search", func(w http.ResponseWriter, r *http.Request) {
		if !methodAllowed(w, r) {
			return
		}
		renderSearchJSON(w, r, pages)
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
func renderPage(w http.ResponseWriter, r *http.Request, version string, page Page, assets assetManifest, docLinks []docLink) {
	t := template.Must(template.New("base").Funcs(assetFuncs(assets)).ParseFS(embedded,
		"templates/base.html",
		"templates/partials/navigation.html",
		"templates/partials/docs-sidebar.html",
		"templates/"+page.Template,
	))
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	if r.Method == http.MethodHead {
		return
	}
	data := pageData{
		Title:    page.Title,
		Path:     page.Path,
		Docs:     strings.HasPrefix(page.Path, "/docs"),
		Eyebrow:  page.Eyebrow,
		Lede:     page.Lede,
		Body:     page.Body,
		Version:  version,
		DocLinks: docLinks,
		Previous: previousDocLink(docLinks, page.Path),
		Next:     nextDocLink(docLinks, page.Path),
	}
	if err := t.ExecuteTemplate(w, "base", data); err != nil {
		return
	}
}

// previousDocLink returns the document that precedes pagePath in the
// catalogue, or nil for the first document and for the hand-written
// overview page.
func previousDocLink(docLinks []docLink, pagePath string) *docLink {
	for i, link := range docLinks {
		if link.Path == pagePath {
			if i == 0 {
				return nil
			}
			prev := docLinks[i-1]
			return &prev
		}
	}
	return nil
}

// nextDocLink returns the document that follows pagePath in the catalogue,
// or nil for the last document and for the hand-written overview page.
func nextDocLink(docLinks []docLink, pagePath string) *docLink {
	for i, link := range docLinks {
		if link.Path == pagePath {
			if i+1 >= len(docLinks) {
				return nil
			}
			next := docLinks[i+1]
			return &next
		}
	}
	return nil
}

// renderSearchJSON answers the spotlight modal's live queries. The scoring
// stays server-side (searchPages); the client only renders path/title/summary.
func renderSearchJSON(w http.ResponseWriter, r *http.Request, pages []Page) {
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	if r.Method == http.MethodHead {
		return
	}
	q := strings.TrimSpace(r.URL.Query().Get("q"))
	matches := searchPages(q, pages)
	results := make([]map[string]string, 0, len(matches))
	for _, page := range matches {
		results = append(results, map[string]string{
			"path":    page.Path,
			"title":   page.Title,
			"summary": page.Summary,
		})
	}
	if err := json.NewEncoder(w).Encode(map[string]any{"query": q, "results": results}); err != nil {
		return
	}
}

// searchPages scores every page against its prebuilt corpus: a title hit
// outranks a lede/keyword hit, which outranks a body hit. Ties keep the
// catalogue order.
func searchPages(query string, pages []Page) []Page {
	needle := strings.ToLower(strings.TrimSpace(query))
	if needle == "" {
		return nil
	}
	type hit struct {
		page  Page
		score int
	}
	var hits []hit
	for _, page := range pages {
		score := 0
		switch {
		case strings.Contains(strings.ToLower(page.Title), needle):
			score = 3
		case strings.Contains(strings.ToLower(page.Summary), needle),
			containsKeyword(page.Keywords, needle):
			score = 2
		case strings.Contains(page.Corpus, needle):
			score = 1
		}
		if score > 0 {
			hits = append(hits, hit{page: page, score: score})
		}
	}
	sort.SliceStable(hits, func(i, j int) bool { return hits[i].score > hits[j].score })
	results := make([]Page, 0, len(hits))
	for _, h := range hits {
		results = append(results, h.page)
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
