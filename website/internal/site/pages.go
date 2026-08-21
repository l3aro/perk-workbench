package site

import (
	"fmt"
	"html/template"
	"strings"
)

// Page describes a renderable page in the Perk Workbench site.
type Page struct {
	Path     string
	Title    string
	Summary  string
	Template string
	Keywords []string

	// Eyebrow, Lede, and Body come from the docs markdown content and are
	// empty for hand-written template pages.
	Eyebrow string
	Lede    string
	Body    template.HTML
	// Corpus is the lowercase search text: title, summary/lede, keywords,
	// and the raw markdown body. Built once at startup.
	Corpus string
}

// PageCatalogue returns the site's pages in navigation order.
func PageCatalogue() []Page {
	return []Page{
		{
			Path:     "/",
			Title:    "Perk Workbench",
			Summary:  "A terminal-native database workbench for exploring data, queries, schemas, and plugins.",
			Template: "pages/home.html",
			Keywords: []string{"database", "SQL", "terminal", "Bubble Tea", "Perk Workbench"},
		},
		{
			Path:     "/demo",
			Title:    "Live demo",
			Summary:  "Run queries against the Chinook SQLite demo in the real Perk Workbench terminal, read-only.",
			Template: "pages/demo.html",
			Keywords: []string{"demo", "live", "terminal", "SQLite", "Chinook", "read-only"},
		},
		{
			Path:     "/docs",
			Title:    "Documentation",
			Summary:  "Learn how to install, connect, query, and extend Perk Workbench.",
			Template: "pages/docs.html",
			Keywords: []string{"documentation", "docs", "Perk Workbench"},
		},
		{
			Path:     "/docs/getting-started",
			Title:    "Getting Started",
			Summary:  "Install Perk Workbench, open a database, and run your first query from the terminal.",
			Template: "pages/getting-started.html",
			Keywords: []string{"install", "quickstart", "first query", "terminal", "database"},
		},
		{
			Path:     "/docs/connections",
			Title:    "Connections",
			Summary:  "Connect to SQLite, MySQL, PostgreSQL, and MongoDB while keeping credentials and driver behavior predictable.",
			Template: "pages/connections.html",
			Keywords: []string{"SQLite", "MySQL", "PostgreSQL", "MongoDB", "drivers", "connection"},
		},
		{
			Path:     "/docs/workspace",
			Title:    "Workspace",
			Summary:  "Navigate schemas, write and execute queries, inspect results, and move through the workbench with keyboard-first controls.",
			Template: "pages/workspace.html",
			Keywords: []string{"workspace", "queries", "schema", "results", "keyboard", "shortcuts"},
		},
		{
			Path:     "/docs/ai",
			Title:    "AI",
			Summary:  "Use AI assistance to understand schemas and shape queries without leaving the terminal-native workspace.",
			Template: "pages/ai.html",
			Keywords: []string{"AI", "queries", "schema", "assistance", "terminal"},
		},
		{
			Path:     "/docs/plugins",
			Title:    "Plugins",
			Summary:  "Extend Perk Workbench with declarative drivers and workspace views through the versioned Perk plugin protocol.",
			Template: "pages/plugins.html",
			Keywords: []string{"plugins", "Perk protocol", "drivers", "workspace views", "extensions"},
		},
	}
}

// LoadPages merges the static catalogue with the rendered docs markdown.
// Docs pages take their title, lede, keywords, and body from the markdown
// file; the catalogue entry only fixes the route order.
func LoadPages() []Page {
	docs := loadDocContent()
	pages := PageCatalogue()
	for i := range pages {
		doc, ok := docs[pages[i].Path]
		if !ok {
			continue
		}
		pages[i].Title = doc.Meta.Title
		pages[i].Summary = doc.Meta.Lede
		pages[i].Eyebrow = doc.Meta.Eyebrow
		pages[i].Lede = doc.Meta.Lede
		pages[i].Keywords = doc.Meta.Keywords
		pages[i].Body = doc.Body
		pages[i].Template = "pages/doc.html"
		pages[i].Corpus = strings.ToLower(strings.Join([]string{
			doc.Meta.Title,
			doc.Meta.Lede,
			strings.Join(doc.Meta.Keywords, " "),
			doc.Text,
		}, "\n"))
	}
	for _, path := range []string{"/", "/demo", "/docs"} {
		if _, ok := docs[path]; ok {
			panic(fmt.Sprintf("site: %s must stay a template page", path))
		}
	}
	return pages
}
