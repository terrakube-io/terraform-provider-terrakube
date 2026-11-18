package provider

import "testing"

// Unit test for the version gate used by the acceptance tests; it runs without TF_ACC.
func TestTerrakubeVersionOlder(t *testing.T) {
	tests := []struct {
		a, b  string
		older bool
	}{
		{"2.33.3", "2.34.0", true},
		{"2.34.0", "2.34.0", false},
		{"2.34.2", "2.34.0", false},
		{"2.35.0", "2.34.9", false},
		{"2.9.0", "2.10.0", true},
		{"1.99.99", "2.0.0", true},
		{"v2.33.3", "2.34.0", true},
		{"2.34.0-beta.1", "2.34.0", false},
	}
	for _, test := range tests {
		older, err := terrakubeVersionOlder(test.a, test.b)
		if err != nil {
			t.Fatalf("%s vs %s: %v", test.a, test.b, err)
		}
		if older != test.older {
			t.Errorf("terrakubeVersionOlder(%q, %q) = %t, want %t", test.a, test.b, older, test.older)
		}
	}

	for _, invalid := range []string{"", "2.34", "2.34.x", "latest", "2.34.0.1"} {
		if _, err := terrakubeVersionOlder(invalid, "2.34.0"); err == nil {
			t.Errorf("terrakubeVersionOlder(%q, ...) should fail", invalid)
		}
	}
}
