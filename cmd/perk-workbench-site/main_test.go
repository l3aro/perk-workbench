package main

import "testing"

func TestParsePort(t *testing.T) {
	tests := []struct {
		name    string
		input   string
		want    int
		wantErr bool
	}{
		{name: "empty defaults to 8080", input: "", want: 8080},
		{name: "valid 1", input: "1", want: 1},
		{name: "valid 65535", input: "65535", want: 65535},
		{name: "malformed", input: "8080x", wantErr: true},
		{name: "nonnumeric", input: "port", wantErr: true},
		{name: "zero", input: "0", wantErr: true},
		{name: "negative", input: "-1", wantErr: true},
		{name: "65536 out of range", input: "65536", wantErr: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := parsePort(tt.input)
			if (err != nil) != tt.wantErr {
				t.Fatalf("parsePort(%q) error = %v, wantErr %v", tt.input, err, tt.wantErr)
			}
			if err == nil && got != tt.want {
				t.Errorf("parsePort(%q) = %d, want %d", tt.input, got, tt.want)
			}
		})
	}
}
