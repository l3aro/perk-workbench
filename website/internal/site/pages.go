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

// PageCatalogue returns the hand-written pages in navigation order. The
// documentation routes are markdown-backed and appended by LoadPages.
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
	}
}

// LoadPages merges the static catalogue with the rendered docs markdown.
// Every markdown document becomes a page in frontmatter order after /docs;
// documents take their title, lede, keywords, and body from the frontmatter
// and rendered body. The static catalogue only fixes the routes that must
// never be markdown-backed.
func LoadPages() []Page {
	docs := loadDocContent()
	pages := PageCatalogue()
	for _, doc := range docs {
		switch doc.Path {
		case "/", "/demo", "/docs":
			panic(fmt.Sprintf("site: %s must stay a template page", doc.Path))
		}
		pages = append(pages, Page{
			Path:     doc.Path,
			Title:    doc.Meta.Title,
			Summary:  doc.Meta.Lede,
			Eyebrow:  doc.Meta.Eyebrow,
			Lede:     doc.Meta.Lede,
			Keywords: doc.Meta.Keywords,
			Body:     doc.Body,
			Template: "pages/doc.html",
			Corpus: strings.ToLower(strings.Join([]string{
				doc.Meta.Title,
				doc.Meta.Lede,
				strings.Join(doc.Meta.Keywords, " "),
				doc.Text,
			}, "\n")),
		})
	}
	return pages
}
