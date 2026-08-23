package schema

import (
	"testing"

	"charm.land/bubbles/v2/list"
)

var benchmarkListFilterSink []list.Rank

func BenchmarkListFilter(b *testing.B) {
	targets := benchmarkSchemaTargets()
	terms := []struct {
		name string
		term string
	}{
		{name: "table-prefix", term: "order*"},
		{name: "table-suffix", term: "*history"},
		{name: "single-character", term: "user?"},
		{name: "schema-prefix", term: "report*"},
	}

	for _, test := range terms {
		b.Run(test.name, func(b *testing.B) {
			b.ReportAllocs()
			for b.Loop() {
				benchmarkListFilterSink = ListFilter(test.term, targets)
			}
		})
	}
}

func benchmarkSchemaTargets() []string {
	databases := []string{"analytics", "billing", "operations", "warehouse"}
	schemas := []string{"public", "reporting", "archive"}
	tables := []string{
		"customers", "customer_addresses", "orders", "order_items", "order_history",
		"invoices", "invoice_lines", "payments", "users", "user_sessions",
		"audit_log", "event_history", "product_catalog", "warehouse_inventory",
	}
	kinds := []string{"table", "view"}
	targets := make([]string, 0, len(databases)*len(schemas)*len(tables))
	for _, database := range databases {
		for _, schema := range schemas {
			for index, table := range tables {
				targets = append(targets, Item{
					Database: database,
					Name:     table,
					Schema:   schema,
					Kind:     kinds[index%len(kinds)],
				}.FilterValue())
			}
		}
	}
	return targets
}
