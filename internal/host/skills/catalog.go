package skills

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
)

// A curated DirSource is also the host's CATALOG of available skills: the bundles
// an operator has vetted into the source directory. `ironctl skill list` enumerates
// it and `ironctl skill remove` un-catalogs a bundle. Installing a cataloged skill
// into an agent group is a separate, gateway-gated step (BuildChangeRequest); these
// ops only manage what is *available*, never what is granted.

// SkillRef identifies one available skill bundle in a catalog.
type SkillRef struct {
	Name    string `json:"name"`
	Version string `json:"version"`
}

// Catalog is a curated Source that can also enumerate and remove the bundles it
// holds — the host-side admin surface behind `ironctl skill list`/`remove`.
// DirSource implements it; a future Git/OCI-backed source may too.
type Catalog interface {
	Source
	List() ([]SkillRef, error)
	Remove(name, version string) error
}

// List returns every available <name>/<version> bundle under the source root (a
// directory containing a skill.yaml). It is read-only and skips anything that does
// not match the <name>/<version>/skill.yaml shape or whose identifiers are invalid,
// so a stray file can never surface as a skill. Results are sorted for stable output.
func (d DirSource) List() ([]SkillRef, error) {
	if d.Root == "" {
		return nil, fmt.Errorf("skills: DirSource has no configured root")
	}
	nameEntries, err := os.ReadDir(d.Root)
	if err != nil {
		return nil, fmt.Errorf("skills: read catalog: %w", err)
	}
	var refs []SkillRef
	for _, ne := range nameEntries {
		if !ne.IsDir() || !validName(ne.Name()) {
			continue
		}
		verEntries, err := os.ReadDir(filepath.Join(d.Root, ne.Name()))
		if err != nil {
			continue // unreadable name dir: skip rather than fail the whole listing
		}
		for _, ve := range verEntries {
			if !ve.IsDir() || !validVersion(ve.Name()) {
				continue
			}
			manifest := filepath.Join(d.Root, ne.Name(), ve.Name(), manifestFileName)
			if fi, err := os.Stat(manifest); err == nil && !fi.IsDir() {
				refs = append(refs, SkillRef{Name: ne.Name(), Version: ve.Name()})
			}
		}
	}
	sort.Slice(refs, func(i, j int) bool {
		if refs[i].Name != refs[j].Name {
			return refs[i].Name < refs[j].Name
		}
		return refs[i].Version < refs[j].Version
	})
	return refs, nil
}

// Remove deletes a bundle from the catalog. An empty version removes every version
// of the skill (the whole <name> directory). name/version are charset-validated and
// the resolved path is confined to the root, so a crafted identifier can never
// delete outside the catalog. Removing an absent bundle is an error (so the caller
// learns it named nothing), not a silent success.
func (d DirSource) Remove(name, version string) error {
	if d.Root == "" {
		return fmt.Errorf("skills: DirSource has no configured root")
	}
	if !validName(name) {
		return fmt.Errorf("skills: invalid skill name %q", name)
	}
	target := filepath.Join(d.Root, name)
	if version != "" {
		if !validVersion(version) {
			return fmt.Errorf("skills: invalid skill version %q", version)
		}
		target = filepath.Join(target, version)
	}
	if !withinRoot(d.Root, target) {
		return fmt.Errorf("skills: resolved path escapes the catalog root")
	}
	if _, err := os.Stat(target); err != nil {
		return fmt.Errorf("skills: %s not in catalog: %w", catalogLabel(name, version), err)
	}
	if err := os.RemoveAll(target); err != nil {
		return fmt.Errorf("skills: remove %s: %w", catalogLabel(name, version), err)
	}
	return nil
}

func catalogLabel(name, version string) string {
	if version == "" {
		return name
	}
	return name + "@" + version
}
