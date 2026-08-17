package site

// Page describes a renderable page in the Perk Workbench site.
type Page struct {
	Path     string
	Title    string
	Summary  string
	Template string
	Keywords []string
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
