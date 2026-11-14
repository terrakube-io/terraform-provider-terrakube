package provider

import "testing"

func TestRSQLQuote(t *testing.T) {
	tests := []struct {
		name  string
		value string
		want  string
	}{
		{name: "plain", value: "production", want: `"production"`},
		{name: "space", value: "team blue", want: `"team blue"`},
		{name: "double quote", value: `a"b`, want: `"a\"b"`},
		{name: "backslash", value: `a\b`, want: `"a\\b"`},
		{name: "quote and backslash", value: `a\b"c`, want: `"a\\b\"c"`},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := rsqlQuote(tt.value); got != tt.want {
				t.Errorf("rsqlQuote(%q) = %s, want %s", tt.value, got, tt.want)
			}
		})
	}
}
