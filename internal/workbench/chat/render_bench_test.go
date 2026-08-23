package chat

import (
	"testing"

	"github.com/l3aro/perk-workbench/internal/ai"
	"github.com/l3aro/perk-workbench/internal/workbench/uikit"
)

func BenchmarkRefreshView(b *testing.B) {
	model := New()
	model.Resize(uikit.Layout{Width: 96, Height: 28})
	run := model.ActiveRun()
	run.Messages = []ai.Message{
		{
			Role:    ai.RoleUser,
			Content: "Compare the latest paid orders with the account totals and explain the outliers.",
		},
		{
			Role:    ai.RoleAssistant,
			Content: "## Order summary\n\n| Account | Orders | Total |\n| --- | ---: | ---: |\n| Acme | 14 | $12,480 |\n| Northwind | 9 | $8,210 |\n\nThe largest variance is in the enterprise segment.",
		},
	}
	run.Loading = true
	stream := benchmarkStreamBuffers()
	run.StreamBuffer = stream[0]
	model.RefreshView()

	var index int
	b.ReportAllocs()
	for b.Loop() {
		run.StreamBuffer = stream[index%len(stream)]
		index++
		model.RefreshView()
	}
}

func benchmarkStreamBuffers() []string {
	return []string{
		"The next step is to inspect the account-level totals.",
		"The next step is to inspect the account-level totals.\n\n| Account | Delta |\n| --- | ---: |\n| Acme | +12% |\n| Northwind | -4% |",
		"The next step is to inspect the account-level totals.\n\n```sql\nSELECT account_id, SUM(total_amount)\nFROM sales.orders\nGROUP BY account_id;\n```",
		"The next step is to inspect the account-level totals.\n\nThe warehouse snapshot is consistent with the query results.",
	}
}
