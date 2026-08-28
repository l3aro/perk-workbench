package site

import (
	"bytes"
	"embed"
	"fmt"
	"html/template"
	"sort"
	"strings"

	"github.com/yuin/goldmark"
	"github.com/yuin/goldmark/extension"
	"github.com/yuin/goldmark/parser"
	"github.com/yuin/goldmark/renderer/html"
	"gopkg.in/yaml.v3"
)

//go:embed content/docs/*.md
var docContent embed.FS

// docMeta is the YAML frontmatter of a docs markdown file. Path is derived
// from the filename (getting-started.md -> /docs/getting-started), so it is
// deliberately not part of the metadata; order fixes the navigation position.
type docMeta struct {
	Title    string   `yaml:"title"`
	Eyebrow  string   `yaml:"eyebrow"`
	Lede     string   `yaml:"lede"`
	Order    int      `yaml:"order"`
	Keywords []string `yaml:"keywords"`
}

// docPage is one rendered markdown document before it is merged into the
// page catalogue. Path is the derived /docs/ route.
type docPage struct {
	Path string
	Meta docMeta
	Body template.HTML
	Text string
}

// loadDocContent renders every embedded markdown doc once at startup and
// returns the documents in frontmatter order. A missing or malformed file,
// a missing required field, or a duplicate order panics: content errors
// must fail the deploy, not degrade navigation or search silently.
func loadDocContent() []docPage {
	entries, err := docContent.ReadDir("content/docs")
	if err != nil {
		panic(fmt.Sprintf("site: read docs content: %v", err))
	}

	md := goldmark.New(
		goldmark.WithExtensions(extension.GFM),
		goldmark.WithRendererOptions(html.WithUnsafe()),
		goldmark.WithParserOptions(parser.WithAutoHeadingID()),
	)

	pages := make([]docPage, 0, len(entries))
	orders := make(map[int]string, len(entries))
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
		if prev, ok := orders[meta.Order]; ok {
			panic(fmt.Sprintf("site: %s: duplicate docs order %d with %s", name, meta.Order, prev))
		}
		orders[meta.Order] = name

		var rendered bytes.Buffer
		if err := md.Convert(body, &rendered); err != nil {
			panic(fmt.Sprintf("site: render %s: %v", name, err))
		}

		pages = append(pages, docPage{
			Path: path,
			Meta: meta,
			Body: template.HTML(rendered.String()),
			// The raw markdown (frontmatter stripped) doubles as the search
			// corpus, so searchable text always matches what was published.
			Text: string(body),
		})
	}
	if len(pages) == 0 {
		panic("site: no docs content found")
	}
	sort.Slice(pages, func(i, j int) bool { return pages[i].Meta.Order < pages[j].Meta.Order })
	return pages
}

// splitFrontmatter separates a leading `---` delimited YAML block from the
// markdown body. A document without a title, lede, or positive order cannot
// be navigated or summarized, so it is rejected at startup.
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
	if meta.Lede == "" {
		return meta, rest, fmt.Errorf("frontmatter: lede is required")
	}
	if meta.Order <= 0 {
		return meta, rest, fmt.Errorf("frontmatter: order must be a positive integer")
	}
	return meta, rest, nil
}
