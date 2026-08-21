package site

import (
	"bytes"
	"embed"
	"fmt"
	"html/template"
	"strings"

	"github.com/yuin/goldmark"
	"github.com/yuin/goldmark/parser"
	"github.com/yuin/goldmark/renderer/html"
	"gopkg.in/yaml.v3"
)

//go:embed content/docs/*.md
var docContent embed.FS

// docMeta is the YAML frontmatter of a docs markdown file. Path is derived
// from the filename (getting-started.md -> /docs/getting-started), so it is
// deliberately not part of the metadata.
type docMeta struct {
	Title    string   `yaml:"title"`
	Eyebrow  string   `yaml:"eyebrow"`
	Lede     string   `yaml:"lede"`
	Keywords []string `yaml:"keywords"`
}

// docPage is one rendered markdown document before it is merged into the
// page catalogue.
type docPage struct {
	Meta docMeta
	Body template.HTML
	Text string
}

// loadDocContent renders every embedded markdown doc once at startup and
// returns the results keyed by page path. A missing or malformed file panics:
// content errors must fail the deploy, not degrade search silently.
func loadDocContent() map[string]docPage {
	entries, err := docContent.ReadDir("content/docs")
	if err != nil {
		panic(fmt.Sprintf("site: read docs content: %v", err))
	}

	md := goldmark.New(
		goldmark.WithRendererOptions(html.WithUnsafe()),
		goldmark.WithParserOptions(parser.WithAutoHeadingID()),
	)

	pages := make(map[string]docPage, len(entries))
	for _, entry := range entries {
		name := entry.Name()
		if !strings.HasSuffix(name, ".md") {
			continue
		}
		slug := strings.TrimSuffix(name, ".md")
		path := "/docs/" + slug

		source, err := docContent.ReadFile("content/docs/" + name)
		if err != nil {
			panic(fmt.Sprintf("site: read %s: %v", name, err))
		}
		meta, body, err := splitFrontmatter(source)
		if err != nil {
			panic(fmt.Sprintf("site: %s: %v", name, err))
		}
		var rendered bytes.Buffer
		if err := md.Convert(body, &rendered); err != nil {
			panic(fmt.Sprintf("site: render %s: %v", name, err))
		}

		pages[path] = docPage{
			Meta: meta,
			Body: template.HTML(rendered.String()),
			// The raw markdown (frontmatter stripped) doubles as the search
			// corpus, so searchable text always matches what was published.
			Text: string(body),
		}
	}
	if len(pages) == 0 {
		panic("site: no docs content found")
	}
	return pages
}

// splitFrontmatter separates a leading `---` delimited YAML block from the
// markdown body.
func splitFrontmatter(source []byte) (docMeta, []byte, error) {
	var meta docMeta
	rest := source
	text := string(source)
	if !strings.HasPrefix(text, "---\n") {
		return meta, rest, fmt.Errorf("missing frontmatter")
	}
	end := strings.Index(text[4:], "\n---")
	if end < 0 {
		return meta, rest, fmt.Errorf("unterminated frontmatter")
	}
	front := text[4 : 4+end]
	rest = source[4+end+1:]
	rest = bytes.TrimPrefix(rest, []byte("\n"))
	if err := yaml.Unmarshal([]byte(front), &meta); err != nil {
		return meta, rest, fmt.Errorf("frontmatter: %w", err)
	}
	if meta.Title == "" {
		return meta, rest, fmt.Errorf("frontmatter: title is required")
	}
	return meta, rest, nil
}
