package app

import (
	tea "charm.land/bubbletea/v2"
	"github.com/l3aro/perk-workbench/internal/log"
	sharedsql "github.com/l3aro/perk-workbench/internal/sql"
)

// schemaForeignKeysAllLoadedMsg delivers the whole-schema foreign-key map.
// tag is the connection generation at load time (a newer open supersedes
// the result) and rev orders same-connection refreshes: a stale snapshot
// whose message arrives after a newer load is dropped.
type schemaForeignKeysAllLoadedMsg struct {
	tag         uint64
	rev         uint64
	foreignKeys map[string][]sharedsql.ForeignKeyInfo
	err         error
}

// schemaIndexesAllLoadedMsg delivers the whole-schema index map.
type schemaIndexesAllLoadedMsg struct {
	tag     uint64
	rev     uint64
	indexes map[string][]sharedsql.IndexInfo
	err     error
}

// loadSchemaForeignKeysAll refreshes the connection-level foreign-key
// cache the focus diagrams read for rings beyond the selected table. Each
// call bumps the cache revision so overlapping refreshes can never land
// out of order.
func (m *Model) loadSchemaForeignKeysAll() tea.Cmd {
	service := m.Database
	if service == nil {
		return nil
	}
	m.schema.foreignKeysRev++
	tag, rev := m.openTag, m.schema.foreignKeysRev
	return func() tea.Msg {
		foreignKeys, err := service.ListForeignKeysAll(m.appContext)
		return schemaForeignKeysAllLoadedMsg{tag: tag, rev: rev, foreignKeys: foreignKeys, err: err}
	}
}

// loadSchemaIndexesAll refreshes the connection-level index cache the
// indexes diagram reads for every card in its focus ring.
func (m *Model) loadSchemaIndexesAll() tea.Cmd {
	service := m.Database
	if service == nil {
		return nil
	}
	m.schema.indexesRev++
	tag, rev := m.openTag, m.schema.indexesRev
	return func() tea.Msg {
		indexes, err := service.ListIndexesAll(m.appContext)
		return schemaIndexesAllLoadedMsg{tag: tag, rev: rev, indexes: indexes, err: err}
	}
}

func (m Model) updateSchemaForeignKeysAll(message schemaForeignKeysAllLoadedMsg) (tea.Model, tea.Cmd) {
	if message.tag != m.openTag || message.rev != m.schema.foreignKeysRev {
		return m, nil // superseded by a newer connection or refresh
	}
	if message.err != nil {
		// The cache is background data: the per-table loads surface their
		// own errors, and the diagram degrades to the selected table only.
		log.Error("loading schema foreign keys", message.err)
		return m, nil
	}
	m.schema.foreignKeysAll = message.foreignKeys
	return m, nil
}

func (m Model) updateSchemaIndexesAll(message schemaIndexesAllLoadedMsg) (tea.Model, tea.Cmd) {
	if message.tag != m.openTag || message.rev != m.schema.indexesRev {
		return m, nil // superseded by a newer connection or refresh
	}
	if message.err != nil {
		log.Error("loading schema indexes", message.err)
		return m, nil
	}
	m.schema.indexesAll = message.indexes
	return m, nil
}
