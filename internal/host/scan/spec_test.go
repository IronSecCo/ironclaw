package scan

import "testing"

func TestTristateString(t *testing.T) {
	tests := []struct {
		name  string
		value Tristate
		want  string
	}{
		{name: "unknown", value: Unknown, want: "unknown"},
		{name: "yes", value: Yes, want: "yes"},
		{name: "no", value: No, want: "no"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.value.String(); got != tt.want {
				t.Fatalf("Tristate(%d).String() = %q, want %q", tt.value, got, tt.want)
			}
		})
	}
}

func TestBoolTri(t *testing.T) {
	tests := []struct {
		name  string
		value bool
		want  Tristate
	}{
		{name: "true", value: true, want: Yes},
		{name: "false", value: false, want: No},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := boolTri(tt.value); got != tt.want {
				t.Fatalf("boolTri(%t) = %v, want %v", tt.value, got, tt.want)
			}
		})
	}
}
