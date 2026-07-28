package version

import (
	"strings"
	"testing"
)

func TestString(t *testing.T) {
	tests := []struct {
		name      string
		version   string
		wantExact string
	}{
		{
			name:      "stamped release tag",
			version:   "v0.1.66",
			wantExact: "v0.1.66",
		},
		{
			name:    "development build",
			version: "dev",
		},
		{
			name:    "empty version",
			version: "",
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			restoreVersion(t, test.version)

			got := String()
			if test.wantExact != "" {
				if got != test.wantExact {
					t.Fatalf("String() = %q, want %q", got, test.wantExact)
				}
				return
			}

			assertDevelopmentVersion(t, got, test.version)
		})
	}
}

func restoreVersion(t *testing.T, value string) {
	t.Helper()

	original := Version
	Version = value
	t.Cleanup(func() {
		Version = original
	})
}

func assertDevelopmentVersion(t *testing.T, got, fallback string) {
	t.Helper()

	if got == fallback {
		return
	}
	if got == "" || got == "(devel)" {
		t.Fatalf("String() returned invalid development identifier %q", got)
	}
	if strings.HasPrefix(got, "dev+") {
		revision := strings.TrimSuffix(strings.TrimPrefix(got, "dev+"), "-dirty")
		if revision == "" || len(revision) > 12 {
			t.Fatalf("String() returned invalid VCS identifier %q", got)
		}
	}
}
