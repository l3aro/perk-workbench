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
