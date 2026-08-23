package sql

import (
	"strings"
	"testing"
)

var benchmarkSQLAnalysisSink SQLAnalysis

func BenchmarkAnalyzeSQL(b *testing.B) {
	value := benchmarkSQLBuffer()
	cursor := strings.LastIndex(value, "AND o.status = 'paid'")
	if cursor < 0 {
		b.Fatal("benchmark SQL cursor marker missing")
	}
	cursor += len("AND o.status = '")
	row := strings.Count(value[:cursor], "\n")
	lineStart := strings.LastIndex(value[:cursor], "\n") + 1
	col := cursor - lineStart

	b.ReportAllocs()
	for b.Loop() {
		benchmarkSQLAnalysisSink = AnalyzeSQL(value, row, col)
	}
}

func benchmarkSQLBuffer() string {
	return `WITH recent_orders AS (
    SELECT o.id, o.customer_id, o.created_at, o.status, o.total_amount
    FROM sales.orders AS o
    WHERE o.created_at >= CURRENT_DATE - INTERVAL '90 days'
      AND o.status IN ('paid', 'shipped', 'complete')
), customer_totals AS (
    SELECT c.id AS customer_id,
           c.account_id,
           c.email,
           COUNT(ro.id) AS order_count,
           SUM(ro.total_amount) AS gross_total,
           MAX(ro.created_at) AS latest_order
    FROM crm.customers AS c
    LEFT JOIN recent_orders AS ro ON ro.customer_id = c.id
    GROUP BY c.id, c.account_id, c.email
), ranked_customers AS (
    SELECT ct.*,
           ROW_NUMBER() OVER (
               PARTITION BY ct.account_id
               ORDER BY ct.gross_total DESC, ct.latest_order DESC
           ) AS account_rank
    FROM customer_totals AS ct
)
SELECT rc.account_id,
       a.name AS account_name,
       rc.customer_id,
       rc.email,
       rc.order_count,
       rc.gross_total,
       rc.latest_order,
       CASE WHEN rc.account_rank = 1 THEN 'top' ELSE 'standard' END AS segment,
       COALESCE(s.name, 'unknown') AS sales_region
FROM ranked_customers AS rc
JOIN crm.accounts AS a ON a.id = rc.account_id
LEFT JOIN crm.sales_regions AS s ON s.id = a.sales_region_id
WHERE rc.order_count >= 2
  AND rc.gross_total > 1000
  AND a.status = 'active'
  AND o.status = 'paid'
ORDER BY rc.gross_total DESC, rc.latest_order DESC
LIMIT 250`
}
