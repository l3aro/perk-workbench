package app

import (
	"testing"

	sharedsql "github.com/l3aro/perk-workbench/internal/sql"
)

var benchmarkAppQueryTextareaViewSink string

func BenchmarkQueryTextareaView(b *testing.B) {
	const query = `-- Recent orders, including customer notes and the latest status.
WITH recent_orders AS (
    SELECT o.id, o.customer_id, o.created_at,
           CASE WHEN o.note IS NULL THEN 'no note' ELSE o.note END AS note,
           /* Keep this comment multiline so the lexer carries state across lines.
              It also mentions a string literal: 'not executable'. */
           o.total_cents / 100.0 AS total
    FROM orders AS o
    WHERE o.created_at >= '2026-01-01'
      AND o.status IN ('paid', 'shipped', 'refunded')
)
SELECT c.name, r.id, r.note, r.total
FROM customers AS c
JOIN recent_orders AS r ON r.customer_id = c.id
WHERE c.email LIKE '%@example.test'
ORDER BY r.created_at DESC, r.id DESC
LIMIT 50;`

	text := newQueryTextarea(96, 12, sharedsql.SQLQueryLanguage)
	text.SetValue(query)
	text.Focus()
	text.input.CursorUp()
	text.input.CursorUp()
	text.input.SetCursorColumn(28)

	b.ReportAllocs()
	for b.Loop() {
		benchmarkAppQueryTextareaViewSink = text.View()
	}
}
