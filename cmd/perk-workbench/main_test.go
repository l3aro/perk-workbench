package main

import (
	"strings"
	"testing"
)

func TestUsage_includesWorkbenchCommand(t *testing.T) {
	if !strings.Contains(usage, "perk-workbench") {
		t.Fatalf("usage = %q, want perk-workbench command", usage)
	}
}

func TestParseTarget(t *testing.T) {
	tests := []struct {
		name       string
		args       []string
		wantTarget string
		wantRO     bool
		wantErr    bool
	}{
		{name: "accepts no target", args: nil, wantTarget: ""},
		{name: "accepts memory target", args: []string{":memory:"}, wantTarget: ":memory:"},
		{name: "accepts one path target", args: []string{"workbench.db"}, wantTarget: "workbench.db"},
		{name: "accepts read-only flag", args: []string{"--read-only", "db.sqlite"}, wantTarget: "db.sqlite", wantRO: true},
		{name: "accepts short read-only flag", args: []string{"-r", "db.sqlite"}, wantTarget: "db.sqlite", wantRO: true},
		{name: "accepts read-only without target", args: []string{"--read-only"}, wantTarget: "", wantRO: true},
		{name: "rejects two targets", args: []string{"first.db", "second.db"}, wantErr: true},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			got, readOnly, err := parseTarget(test.args)

			if test.wantErr {
				if err == nil {
					t.Fatal("parseTarget() error = nil, want an error")
				}
				return
			}
			if err != nil {
				t.Fatalf("parseTarget() error = %v", err)
			}
			if got != test.wantTarget {
				t.Fatalf("parseTarget() target = %q, want %q", got, test.wantTarget)
			}
			if readOnly != test.wantRO {
				t.Fatalf("parseTarget() readOnly = %v, want %v", readOnly, test.wantRO)
			}
		})
	}
}
