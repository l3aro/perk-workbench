package sql

import (
	"testing"
)

func TestValidateColumnAttributeChange_nilIsAllowed(t *testing.T) {
	if err := ValidateColumnAttributeChange(nil, ""); err != nil {
		t.Fatalf("ValidateColumnAttributeChange(nil, \"\") = %v, want nil", err)
	}
}

func TestValidateColumnAttributeChange_sameValueIsAllowed(t *testing.T) {
	attrs := "GENERATED STORED"
	if err := ValidateColumnAttributeChange(&attrs, "GENERATED STORED"); err != nil {
		t.Fatalf("ValidateColumnAttributeChange(same) = %v, want nil", err)
	}
}

func TestValidateColumnAttributeChange_differentValueRejected(t *testing.T) {
	attrs := "COMMENT 'foo'"
	if err := ValidateColumnAttributeChange(&attrs, "GENERATED STORED"); err == nil {
		t.Fatal("ValidateColumnAttributeChange(different) = nil, want error")
	}
}

func TestValidateColumnAttributeChange_emptyClearedRejected(t *testing.T) {
	empty := ""
	if err := ValidateColumnAttributeChange(&empty, "GENERATED STORED"); err == nil {
		t.Fatal("ValidateColumnAttributeChange(cleared) = nil, want error")
	}
}

func TestValidateColumnDef_valid(t *testing.T) {
	if err := ValidateColumnDef(ColumnDef{Name: "col", Type: "TEXT"}); err != nil {
		t.Fatalf("ValidateColumnDef(valid) = %v, want nil", err)
	}
}

func TestValidateColumnDef_missingName(t *testing.T) {
	if err := ValidateColumnDef(ColumnDef{Name: "", Type: "INTEGER"}); err == nil {
		t.Fatal("ValidateColumnDef(empty name) = nil, want error")
	}
}

func TestValidateColumnDef_missingType(t *testing.T) {
	if err := ValidateColumnDef(ColumnDef{Name: "col", Type: ""}); err == nil {
		t.Fatal("ValidateColumnDef(empty type) = nil, want error")
	}
}

func TestValidateColumnDef_withDefault(t *testing.T) {
	def := "42"
	if err := ValidateColumnDef(ColumnDef{Name: "col", Type: "INTEGER", DefaultValue: &def}); err != nil {
		t.Fatalf("ValidateColumnDef(with default) = %v, want nil", err)
	}
}

func TestValidateColumnDef_withDefaultContainingSemicolon(t *testing.T) {
	def := "1; DROP TABLE items"
	if err := ValidateColumnDef(ColumnDef{Name: "col", Type: "INTEGER", DefaultValue: &def}); err == nil {
		t.Fatal("ValidateColumnDef(semicolon default) = nil, want error")
	}
}

func TestValidateColumnDef_controlCharacterName(t *testing.T) {
	if err := ValidateColumnDef(ColumnDef{Name: "col\x00", Type: "TEXT"}); err == nil {
		t.Fatal("ValidateColumnDef(control name) = nil, want error")
	}
}

func TestValidateColumnDef_unsupportedTypeChars(t *testing.T) {
	if err := ValidateColumnDef(ColumnDef{Name: "col", Type: "VARCHAR(@)"}); err == nil {
		t.Fatal("ValidateColumnDef(bad type chars) = nil, want error")
	}
}
