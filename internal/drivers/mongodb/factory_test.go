package mongodb

import (
	"context"
	"strings"
	"testing"

	"github.com/l3aro/perk-workbench-plugin-sdk-go/driver"
)

func TestFactoryCapabilities_advertiseMongoTargetAndWorkspace(t *testing.T) {
	capabilities := (Factory{}).Capabilities()
	if err := driver.ValidateCapabilities(capabilities); err != nil {
		t.Fatalf("ValidateCapabilities() = %v", err)
	}
	wantTargets := []driver.TargetPattern{
		{Prefix: "mongo:"},
		{Prefix: "mongodb://", KeepTarget: true},
		{Prefix: "mongodb+srv://", KeepTarget: true},
	}
	if len(capabilities.Targets) != len(wantTargets) {
		t.Fatalf("targets = %#v, want %#v", capabilities.Targets, wantTargets)
	}
	for i := range wantTargets {
		if capabilities.Targets[i] != wantTargets[i] {
			t.Fatalf("targets = %#v, want %#v", capabilities.Targets, wantTargets)
		}
	}
	if capabilities.Workspace == nil || len(capabilities.Workspace.CustomViews) != 1 || capabilities.Workspace.CustomViews[0].ID != workspaceStatsViewID {
		t.Fatalf("workspace = %#v, want stats custom view", capabilities.Workspace)
	}
}

func TestFactoryOpenFailure_normalizesConnectionWithoutSecrets(t *testing.T) {
	target := "mongodb://alice:secret@%zz"
	_, err := (Factory{}).Open(context.Background(), target)
	if err == nil {
		t.Fatal("Open() error = nil, want invalid-target failure")
	}
	op, ok := err.(*driver.OperationError)
	if !ok {
		t.Fatalf("Open() error type = %T, want *driver.OperationError", err)
	}
	if op.Kind != driver.KindConnection {
		t.Fatalf("Open() error kind = %q, want connection", op.Kind)
	}
	if op.Message != "opening MongoDB connection failed" {
		t.Fatalf("Open() error message = %q, want generic message", op.Message)
	}
	if strings.Contains(op.Message, target) || strings.Contains(op.Message, "alice") || strings.Contains(op.Message, "secret") {
		t.Fatalf("Open() error leaked target or credentials: %q", op.Message)
	}
}

func TestDocumentWrite_missingPayloadIsValidation(t *testing.T) {
	service := &sessionService{}
	_, err := service.DocumentWrite(context.Background(), driver.DocumentWriteRequest{Operation: driver.DocumentWriteInsert})
	if err == nil {
		t.Fatal("DocumentWrite() error = nil, want missing-document validation")
	}
	op, ok := err.(*driver.OperationError)
	if !ok || op.Kind != driver.KindValidation {
		t.Fatalf("DocumentWrite() error = %#v, want validation OperationError", err)
	}
}
