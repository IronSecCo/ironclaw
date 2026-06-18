package webui

import (
	"io/fs"
	"testing"
)

// TestAssetsContainsShell guards the embed: a build that fails to compile the
// static tree in (e.g. someone renames the directory to dist/, which .gitignore
// excludes) must fail CI here rather than ship a binary that 404s the console.
func TestAssetsContainsShell(t *testing.T) {
	want := []string{"index.html", "app.js", "style.css"}
	assets := Assets()
	for _, name := range want {
		if _, err := fs.Stat(assets, name); err != nil {
			t.Errorf("embedded console missing %q: %v", name, err)
		}
	}
}

// TestIndexReferencesApp is a cheap smoke check that the shell actually wires in
// the script/style so the embed is a working page, not three orphaned files.
func TestIndexReferencesApp(t *testing.T) {
	b, err := fs.ReadFile(Assets(), "index.html")
	if err != nil {
		t.Fatalf("read index.html: %v", err)
	}
	for _, ref := range []string{"app.js", "style.css"} {
		if !contains(b, ref) {
			t.Errorf("index.html does not reference %q", ref)
		}
	}
}

func contains(haystack []byte, needle string) bool {
	n := []byte(needle)
	for i := 0; i+len(n) <= len(haystack); i++ {
		if string(haystack[i:i+len(n)]) == string(n) {
			return true
		}
	}
	return false
}
