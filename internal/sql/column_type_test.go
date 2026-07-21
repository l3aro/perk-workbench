package sql

import "testing"

func TestColumnTypes_mysqlOmitsDeprecatedZeroFill(t *testing.T) {
	// Given
	typeDefinitions := ColumnTypes(DatabaseInfo{Product: "MySQL"})

	// When
	for _, typeDefinition := range typeDefinitions {
		if typeDefinition.Name == "INT ZEROFILL" || typeDefinition.Name == "INT UNSIGNED ZEROFILL" {
			t.Fatalf("deprecated type = %q, want omitted", typeDefinition.Name)
		}
	}

	// Then
	foundUnsigned := false
	for _, typeDefinition := range typeDefinitions {
		if typeDefinition.Name == "INT UNSIGNED" {
			foundUnsigned = true
		}
	}
	if !foundUnsigned {
		t.Fatal("INT UNSIGNED is missing")
	}
}

func TestColumnTypeDeclaration_buildsParameterizedType(t *testing.T) {
	// Given
	typeDefinition := ColumnType{Name: "DECIMAL", Parameters: []ColumnTypeParameter{{Name: "Precision", Default: "10"}, {Name: "Scale", Default: "2"}}}

	// When
	declaration, err := typeDefinition.Declaration([]string{"12", "2"})

	// Then
	if err != nil {
		t.Fatalf("Declaration() error = %v", err)
	}
	if declaration != "DECIMAL(12,2)" {
		t.Errorf("declaration = %q, want DECIMAL(12,2)", declaration)
	}
}

func TestColumnTypeDeclaration_rejectsScaleAbovePrecision(t *testing.T) {
	// Given
	typeDefinition := ColumnType{Name: "DECIMAL", Parameters: []ColumnTypeParameter{{Name: "Precision", Default: "10"}, {Name: "Scale", Default: "2"}}}

	// When
	_, err := typeDefinition.Declaration([]string{"2", "3"})

	// Then
	if err == nil {
		t.Fatal("Declaration() error = nil, want scale validation error")
	}
}

func TestIsNumericColumnType_recognizes_database_numeric_types(t *testing.T) {
	for typeName, want := range map[string]bool{
		"INTEGER":         true,
		"DECIMAL(10,2)":   true,
		"BIGINT UNSIGNED": true,
		"TEXT":            false,
		"BOOLEAN":         false,
		"":                false,
	} {
		// Given
		// When
		got := IsNumericColumnType(typeName)

		// Then
		if got != want {
			t.Errorf("IsNumericColumnType(%q) = %t, want %t", typeName, got, want)
		}
	}
}
