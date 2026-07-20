package main

import "testing"

func TestParseTarget(t *testing.T) {
	tests := []struct {
		name    string
		args    []string
		want    string
		wantErr bool
	}{
		{name: "accepts no target", args: nil, want: ""},
		{name: "accepts memory target", args: []string{":memory:"}, want: ":memory:"},
		{name: "accepts one path target", args: []string{"workbench.db"}, want: "workbench.db"},
		{name: "rejects two targets", args: []string{"first.db", "second.db"}, wantErr: true},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			// Given
			// When
			got, err := parseTarget(test.args)

			// Then
			if test.wantErr {
				if err == nil {
					t.Fatal("parseTarget() error = nil, want an error")
				}
				return
			}
			if err != nil {
				t.Fatalf("parseTarget() error = %v", err)
			}
			if got != test.want {
				t.Fatalf("parseTarget() = %q, want %q", got, test.want)
			}
		})
	}
}
