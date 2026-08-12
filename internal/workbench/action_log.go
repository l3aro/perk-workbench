package workbench

import (
	"strings"

	sharedsql "github.com/l3aro/perk-workbench/internal/sql"
)

func (m Model) quoteIdentifier(name string) string {
	quote := `"`
	if m.databaseInfo.Product == "MySQL" {
		quote = "`"
	}
	return quote + strings.ReplaceAll(name, quote, quote+quote) + quote
}

func (m Model) actionIdentifier(name string) string {
	if m.databaseInfo.Product == "MySQL" || m.databaseInfo.Product == "PostgreSQL" {
		if database, table, found := strings.Cut(name, "."); found {
			return m.quoteIdentifier(database) + "." + m.quoteIdentifier(table)
		}
	}
	return m.quoteIdentifier(name)
}

func (m Model) indexChangeStatement(table, previous string, change sharedsql.IndexChange) string {
	columns := make([]string, len(change.Columns))
	for index, column := range change.Columns {
		columns[index] = m.actionIdentifier(strings.TrimSpace(column))
	}
	quotedTable := m.actionIdentifier(table)
	if change.PrimaryKey {
		if previous == "" {
			return "ALTER TABLE " + quotedTable + " ADD PRIMARY KEY (" + strings.Join(columns, ", ") + ")"
		}
		if m.databaseInfo.Product == "PostgreSQL" {
			return "ALTER TABLE " + quotedTable + " DROP CONSTRAINT " + m.actionIdentifier(previous) + "; ALTER TABLE " + quotedTable + " ADD PRIMARY KEY (" + strings.Join(columns, ", ") + ")"
		}
		return "ALTER TABLE " + quotedTable + " DROP PRIMARY KEY, ADD PRIMARY KEY (" + strings.Join(columns, ", ") + ")"
	}
	statement := "CREATE INDEX "
	if change.Unique {
		statement = "CREATE UNIQUE INDEX "
	}
	statement += m.actionIdentifier(change.Name) + " ON " + quotedTable + " (" + strings.Join(columns, ", ") + ")"
	if previous == "" {
		return statement
	}
	return m.dropIndexStatement(table, previous) + "; " + statement
}

func (m Model) dropIndexStatement(table, name string) string {
	if m.databaseInfo.Product == "PostgreSQL" && strings.HasSuffix(name, "_pkey") {
		return "ALTER TABLE " + m.actionIdentifier(table) + " DROP CONSTRAINT " + m.actionIdentifier(name)
	}
	if name == "PRIMARY" {
		return "ALTER TABLE " + m.actionIdentifier(table) + " DROP PRIMARY KEY"
	}
	statement := "DROP INDEX " + m.actionIdentifier(name)
	if m.databaseInfo.Product == "MySQL" {
		statement += " ON " + m.actionIdentifier(table)
	}
	return statement
}

func (m Model) foreignKeyChangeStatement(table, previous string, change sharedsql.ForeignKeyChange) string {
	columns := make([]string, len(change.Columns))
	references := make([]string, len(change.ReferenceColumns))
	for index := range change.Columns {
		columns[index] = m.actionIdentifier(strings.TrimSpace(change.Columns[index]))
		references[index] = m.actionIdentifier(strings.TrimSpace(change.ReferenceColumns[index]))
	}
	statement := "ALTER TABLE " + m.actionIdentifier(table) + " ADD FOREIGN KEY (" + strings.Join(columns, ", ") + ") REFERENCES " + m.actionIdentifier(change.ReferenceTable) + " (" + strings.Join(references, ", ") + ") ON DELETE " + change.OnDelete + " ON UPDATE " + change.OnUpdate
	if previous == "" {
		return statement
	}
	return m.dropForeignKeyStatement(table, previous) + "; " + statement
}

func (m Model) dropForeignKeyStatement(table, previous string) string {
	if m.databaseInfo.Product == "PostgreSQL" {
		return "ALTER TABLE " + m.actionIdentifier(table) + " DROP CONSTRAINT " + m.actionIdentifier(previous)
	}
	return "ALTER TABLE " + m.actionIdentifier(table) + " DROP FOREIGN KEY " + m.actionIdentifier(previous)
}

func (m Model) columnChangeStatement(table string, change sharedsql.ColumnChange) string {
	quotedTable := m.actionIdentifier(table)
	if !m.structure.columnForm.typeChanged && !m.structure.columnForm.hadDefault && change.DefaultValue == nil && change.Nullable == m.structure.columnForm.values.nullable && (change.Attributes == nil || *change.Attributes == m.structure.columnForm.originalAttributes) {
		return "ALTER TABLE " + quotedTable + " RENAME COLUMN " + m.actionIdentifier(change.PreviousName) + " TO " + m.actionIdentifier(change.Name)
	}
	statement := "ALTER TABLE " + quotedTable + " ALTER COLUMN " + m.actionIdentifier(change.PreviousName) + " TYPE " + strings.TrimSpace(change.Type)
	if change.Nullable {
		statement += " NULL"
	} else {
		statement += " NOT NULL"
	}
	if change.DefaultValue != nil {
		statement += " DEFAULT " + *change.DefaultValue
	}
	if change.Attributes != nil && *change.Attributes != "" {
		statement += " " + *change.Attributes
	}
	return statement
}

func (m Model) columnAddStatement(table string, def sharedsql.ColumnDef) string {
	quotedTable := m.actionIdentifier(table)
	statement := "ALTER TABLE " + quotedTable + " ADD COLUMN " + m.actionIdentifier(def.Name) + " " + strings.TrimSpace(def.Type)
	if !def.Nullable {
		statement += " NOT NULL"
	}
	if def.DefaultValue != nil {
		statement += " DEFAULT " + *def.DefaultValue
	}
	if def.Attributes != nil && *def.Attributes != "" {
		statement += " " + *def.Attributes
	}
	return statement
}

func (m Model) dropColumnStatement(table, name string) string {
	return "ALTER TABLE " + m.actionIdentifier(table) + " DROP COLUMN " + m.actionIdentifier(name)
}
