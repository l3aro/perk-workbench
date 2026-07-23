package sql

import (
	"fmt"
	"strconv"
	"strings"
)

type ColumnTypeParameter struct {
	Name    string
	Default string
}

type ColumnType struct {
	Name       string
	Parameters []ColumnTypeParameter
}

func ColumnTypes(info DatabaseInfo) []ColumnType {
	length := ColumnTypeParameter{Name: "Length", Default: "255"}
	decimal := []ColumnTypeParameter{{Name: "Precision", Default: "10"}, {Name: "Scale", Default: "2"}}
	if info.Product == "MySQL" {
		return []ColumnType{
			{Name: "TINYINT"}, {Name: "TINYINT UNSIGNED"},
			{Name: "SMALLINT"}, {Name: "SMALLINT UNSIGNED"},
			{Name: "MEDIUMINT"}, {Name: "MEDIUMINT UNSIGNED"},
			{Name: "INT"}, {Name: "INT UNSIGNED"},
			{Name: "BIGINT"}, {Name: "BIGINT UNSIGNED"},
			{Name: "DECIMAL", Parameters: decimal}, {Name: "NUMERIC", Parameters: decimal},
			{Name: "FLOAT"}, {Name: "DOUBLE"},
			{Name: "CHAR", Parameters: []ColumnTypeParameter{length}}, {Name: "VARCHAR", Parameters: []ColumnTypeParameter{length}},
			{Name: "BINARY", Parameters: []ColumnTypeParameter{length}}, {Name: "VARBINARY", Parameters: []ColumnTypeParameter{length}},
			{Name: "TEXT"}, {Name: "BLOB"},
			{Name: "DATE"}, {Name: "TIME"}, {Name: "DATETIME"}, {Name: "TIMESTAMP"}, {Name: "BOOLEAN"}, {Name: "JSON"},
		}
	}
	if info.Product == "PostgreSQL" {
		return []ColumnType{
			{Name: "SMALLINT"}, {Name: "INTEGER"}, {Name: "BIGINT"},
			{Name: "NUMERIC", Parameters: decimal}, {Name: "DECIMAL", Parameters: decimal},
			{Name: "REAL"}, {Name: "DOUBLE PRECISION"},
			{Name: "CHAR", Parameters: []ColumnTypeParameter{length}}, {Name: "VARCHAR", Parameters: []ColumnTypeParameter{length}}, {Name: "TEXT"},
			{Name: "BYTEA"}, {Name: "BOOLEAN"}, {Name: "DATE"}, {Name: "TIME"}, {Name: "TIMESTAMP"}, {Name: "TIMESTAMPTZ"}, {Name: "UUID"}, {Name: "JSONB"},
		}
	}
	return []ColumnType{
		{Name: "INTEGER"}, {Name: "REAL"}, {Name: "TEXT"}, {Name: "BLOB"},
		{Name: "NUMERIC", Parameters: decimal}, {Name: "DECIMAL", Parameters: decimal},
		{Name: "CHAR", Parameters: []ColumnTypeParameter{length}}, {Name: "VARCHAR", Parameters: []ColumnTypeParameter{length}},
		{Name: "DATE"}, {Name: "DATETIME"}, {Name: "BOOLEAN"},
	}
}

func (t ColumnType) Declaration(values []string) (string, error) {
	if len(values) != len(t.Parameters) {
		return "", fmt.Errorf("%s requires %d parameters", t.Name, len(t.Parameters))
	}
	if len(values) == 0 {
		return t.Name, nil
	}
	parsed := make([]int, len(values))
	for index, value := range values {
		number, err := strconv.Atoi(strings.TrimSpace(value))
		if err != nil || number < 0 || (t.Parameters[index].Name != "Scale" && number == 0) {
			return "", fmt.Errorf("%s must be a valid %s", t.Parameters[index].Name, positiveName(t.Parameters[index].Name))
		}
		parsed[index] = number
	}
	if len(parsed) == 2 && t.Parameters[0].Name == "Precision" && parsed[1] > parsed[0] {
		return "", fmt.Errorf("scale cannot exceed precision")
	}
	return t.Name + "(" + strings.Join(values, ",") + ")", nil
}

func MatchColumnType(types []ColumnType, declaration string) (int, []string, bool) {
	declaration = strings.TrimSpace(declaration)
	for index, typeDefinition := range types {
		if len(typeDefinition.Parameters) == 0 && strings.EqualFold(typeDefinition.Name, declaration) {
			return index, nil, true
		}
		if len(typeDefinition.Parameters) == 0 || len(declaration) <= len(typeDefinition.Name) || !strings.EqualFold(declaration[:len(typeDefinition.Name)], typeDefinition.Name) {
			continue
		}
		remainder := strings.TrimSpace(declaration[len(typeDefinition.Name):])
		if len(remainder) < 3 || remainder[0] != '(' || remainder[len(remainder)-1] != ')' {
			continue
		}
		values := strings.Split(remainder[1:len(remainder)-1], ",")
		if len(values) != len(typeDefinition.Parameters) {
			continue
		}
		if _, err := typeDefinition.Declaration(values); err == nil {
			return index, values, true
		}
	}
	return 0, nil, false
}

func IsNumericColumnType(typeName string) bool {
	typeName = strings.ToUpper(strings.TrimSpace(typeName))
	typeName, _, _ = strings.Cut(typeName, "(")
	typeName, _, _ = strings.Cut(typeName, " ")
	switch typeName {
	case "TINYINT", "SMALLINT", "MEDIUMINT", "INT", "INTEGER", "BIGINT", "DECIMAL", "NUMERIC", "FLOAT", "DOUBLE", "REAL":
		return true
	default:
		return false
	}
}

func positiveName(name string) string {
	if name == "Scale" {
		return "non-negative " + strings.ToLower(name)
	}
	return "positive " + strings.ToLower(name)
}
