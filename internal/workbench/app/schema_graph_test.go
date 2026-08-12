package app

import (
	"context"
	"testing"

	sharedsql "github.com/l3aro/perk-workbench/internal/sql"
)

// sequenceFKService returns a different foreign-key snapshot per call, so
// a test can tell which dispatch a delivered message came from.
type sequenceFKService struct {
	sharedsql.Service
	fkResults    []map[string][]sharedsql.ForeignKeyInfo
	indexResults []map[string][]sharedsql.IndexInfo
}

func (s *sequenceFKService) ListForeignKeysAll(context.Context) (map[string][]sharedsql.ForeignKeyInfo, error) {
	result := s.fkResults[0]
	s.fkResults = s.fkResults[1:]
	return result, nil
}

func (s *sequenceFKService) ListIndexesAll(context.Context) (map[string][]sharedsql.IndexInfo, error) {
	result := s.indexResults[0]
	s.indexResults = s.indexResults[1:]
	return result, nil
}

func TestSchemaGraphCache_overlappingRefreshesKeepNewest(t *testing.T) {
	// Given — two overlapping refreshes per cache; the second dispatches
	// while the first is still in flight.
	model := readyModel(t)
	model.Database = &sequenceFKService{
		fkResults: []map[string][]sharedsql.ForeignKeyInfo{
			{"stale": {{Columns: []string{"old_id"}, ReferenceTable: "old"}}},
			{"fresh": {{Columns: []string{"new_id"}, ReferenceTable: "new"}}},
		},
		indexResults: []map[string][]sharedsql.IndexInfo{
			{"stale": {{Name: "idx_old", Columns: []string{"old_id"}}}},
			{"fresh": {{Name: "idx_new", Columns: []string{"new_id"}}}},
		},
	}
	firstFK := model.loadSchemaForeignKeysAll()
	secondFK := model.loadSchemaForeignKeysAll()
	firstIndexes := model.loadSchemaIndexesAll()
	secondIndexes := model.loadSchemaIndexesAll()

	// When — the newer messages are delivered first, the older ones late.
	// The sequence service pops per call, so capture the older snapshots
	// before the newer ones.
	olderFK := firstFK().(schemaForeignKeysAllLoadedMsg)
	newerFK := secondFK().(schemaForeignKeysAllLoadedMsg)
	olderIndexes := firstIndexes().(schemaIndexesAllLoadedMsg)
	newerIndexes := secondIndexes().(schemaIndexesAllLoadedMsg)
	updated, _ := model.Update(newerFK)
	model = updated.(Model)
	updated, _ = model.Update(newerIndexes)
	model = updated.(Model)
	updated, _ = model.Update(olderFK)
	model = updated.(Model)
	updated, _ = model.Update(olderIndexes)
	model = updated.(Model)

	// Then — the stale snapshots never replace the newer cache.
	if got := model.schema.foreignKeysAll["fresh"]; len(got) != 1 {
		t.Fatalf("foreign-key cache = %v, want the fresh snapshot", model.schema.foreignKeysAll)
	}
	if _, stale := model.schema.foreignKeysAll["stale"]; stale {
		t.Fatalf("foreign-key cache contains the stale snapshot: %v", model.schema.foreignKeysAll)
	}
	if got := model.schema.indexesAll["fresh"]; len(got) != 1 {
		t.Fatalf("index cache = %v, want the fresh snapshot", model.schema.indexesAll)
	}
	if _, stale := model.schema.indexesAll["stale"]; stale {
		t.Fatalf("index cache contains the stale snapshot: %v", model.schema.indexesAll)
	}
}

func TestSchemaGraphCache_handlerDropsNonCurrentRevision(t *testing.T) {
	// Given — a newer refresh (rev 2) is already in flight.
	model := readyModel(t)
	model.schema.foreignKeysRev = 2
	model.schema.indexesRev = 2
	staleFK := schemaForeignKeysAllLoadedMsg{tag: model.openTag, rev: 1, foreignKeys: map[string][]sharedsql.ForeignKeyInfo{"stale": {{Columns: []string{"old_id"}, ReferenceTable: "old"}}}}
	staleIndexes := schemaIndexesAllLoadedMsg{tag: model.openTag, rev: 1, indexes: map[string][]sharedsql.IndexInfo{"stale": {{Name: "idx_old", Columns: []string{"old_id"}}}}}
	currentFK := schemaForeignKeysAllLoadedMsg{tag: model.openTag, rev: 2, foreignKeys: map[string][]sharedsql.ForeignKeyInfo{"current": {{Columns: []string{"new_id"}, ReferenceTable: "new"}}}}
	currentIndexes := schemaIndexesAllLoadedMsg{tag: model.openTag, rev: 2, indexes: map[string][]sharedsql.IndexInfo{"current": {{Name: "idx_new", Columns: []string{"new_id"}}}}}

	// When — the stale rev-1 messages arrive first.
	updated, _ := model.Update(staleFK)
	model = updated.(Model)
	updated, _ = model.Update(staleIndexes)
	model = updated.(Model)

	// Then — the caches stay empty.
	if model.schema.foreignKeysAll != nil || model.schema.indexesAll != nil {
		t.Fatalf("stale revisions wrote the caches: fk=%v indexes=%v", model.schema.foreignKeysAll, model.schema.indexesAll)
	}

	// When — the current rev-2 messages arrive.
	updated, _ = model.Update(currentFK)
	model = updated.(Model)
	updated, _ = model.Update(currentIndexes)
	model = updated.(Model)

	// Then — they apply.
	if len(model.schema.foreignKeysAll) != 1 || model.schema.foreignKeysAll["current"] == nil {
		t.Fatalf("current foreign-key revision not applied: %v", model.schema.foreignKeysAll)
	}
	if len(model.schema.indexesAll) != 1 || model.schema.indexesAll["current"] == nil {
		t.Fatalf("current index revision not applied: %v", model.schema.indexesAll)
	}
}

func TestSchemaGraphCache_supersededConnectionIsDropped(t *testing.T) {
	// Given — a load dispatched under the previous connection generation.
	model := readyModel(t)
	oldTag := model.openTag
	model.openTag++
	stale := schemaForeignKeysAllLoadedMsg{tag: oldTag, rev: 1, foreignKeys: map[string][]sharedsql.ForeignKeyInfo{"old": nil}}

	// When
	updated, _ := model.Update(stale)
	model = updated.(Model)

	// Then — the cache stays empty.
	if model.schema.foreignKeysAll != nil {
		t.Fatalf("superseded connection wrote the cache: %v", model.schema.foreignKeysAll)
	}
}
