package site

import (
	"embed"
	"html/template"
	"io/fs"
	"net/http"
)

//go:embed assets templates
var embedded embed.FS

var pages = template.Must(template.ParseFS(
	embedded,
	"templates/base.html",
	"templates/pages/*.html",
	"templates/partials/*.html",
))

type pageData struct {
	Title       string
	Version     string
	Description string
	Path        string
}

// New returns the website HTTP handler.
func New(version string) http.Handler {
	assets, err := fs.Sub(embedded, "assets")
	if err != nil {
		panic(err)
	}

	mux := http.NewServeMux()
	static := http.StripPrefix("/static/", http.FileServer(http.FS(assets)))
	mux.Handle("/static/", noCache(static))
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		data := pageData{
			Title:       "Perk Workbench",
			Version:     version,
			Description: "A terminal-native database workbench.",
			Path:        r.URL.Path,
		}
		if err := pages.ExecuteTemplate(w, "base", data); err != nil {
			http.Error(w, "template rendering failed", http.StatusInternalServerError)
		}
	})
	return mux
}

func noCache(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Cache-Control", "no-cache")
		w.Header().Set("Pragma", "no-cache")
		w.Header().Set("Expires", "0")
		next.ServeHTTP(w, r)
	})
}
