package isolation

import (
	"errors"
	"fmt"
	"testing"
)

func TestIsImageMissing(t *testing.T) {
	cases := []struct {
		name string
		err  error
		want bool
	}{
		{"nil", nil, false},
		{"docker 404 no such image", errors.New(
			"host/isolation: docker create ic-sbx-ses_1: docker api POST /containers/create?name=ic-sbx-ses_1: 404 Not Found: {\"message\":\"No such image: ironclaw-sandbox:latest\"}"), true},
		{"wrapped no such image", fmt.Errorf("launch: %w", errors.New("No such image: foo")), true},
		{"containerd manifest unknown", errors.New("failed to pull: manifest unknown"), true},
		{"image not found", errors.New("image ironclaw-sandbox:latest not found in store"), true},
		{"unrelated start failure", errors.New("host/isolation: docker start ic-sbx-ses_1: 500 Internal Server Error"), false},
		{"rootfs error", errors.New("rootfs not provisioned"), false},
		{"plain not found without image", errors.New("container not found"), false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := IsImageMissing(c.err); got != c.want {
				t.Fatalf("IsImageMissing(%v) = %v, want %v", c.err, got, c.want)
			}
		})
	}
}
