package scan

import (
	"encoding/json"
	"fmt"
	"strings"
)

// --------------------------------------------------------------------------- //
// Image adapter (IRO-712).
//
// An IMAGE is not a container. It carries the build-time config (USER, ENV, CMD,
// ports, labels) and nothing else: no capability set, no seccomp profile, no
// network mode, no mounts, no namespaces. Six of the seven graded containment
// dimensions are decided at `docker run` time and are simply not present in an
// image manifest.
//
// This adapter therefore does NOT reuse specFromDockerLike. That function is
// correct for containers and load-bearing precisely because it resolves absence
// to a concrete value (no seccomp SecurityOpt on a RUNNING container really does
// mean the default profile is active; an empty Mounts list really does mean no
// docker.sock). Feeding image data through it is what produced IRO-711: it
// overwrote the fail-closed Unknown tristate with an affirmative "hardened" claim
// on six dimensions, awarding 40 points of PASS about a container that never
// existed. The only safe split is a separate adapter that leaves every
// unobservable posture at Unknown and a mode that says so out loud.
// --------------------------------------------------------------------------- //

// imageInspect is the subset of `docker image inspect` / `podman image inspect` /
// `nerdctl image inspect` we can grade. All three emit an OCI image config under
// Config; only User is a containment posture. The rest of the schema is
// deliberately not modelled: there is nothing else in an image config that
// decides a containment dimension, and parsing more would invite exactly the
// "looks like enough to grade" mistake this adapter exists to prevent.
type imageInspect struct {
	ID          string   `json:"Id"`
	RepoTags    []string `json:"RepoTags"`
	RepoDigests []string `json:"RepoDigests"`
	Config      struct {
		User string `json:"User"`
	} `json:"Config"`
}

// SpecFromImageInspect parses `<runtime> image inspect <ref>` output into a Spec
// in ModeImage. source is the runtime label ("docker"|"podman"|"nerdctl") and ref
// is the reference the caller asked for, used as the report target so the output
// names what the user typed rather than a resolved digest.
//
// Every run-time posture is left at Unknown ON PURPOSE. The scorer reports those
// dimensions as N/A rather than grading them, so this adapter can never claim a
// boundary holds on an artifact that does not have that boundary.
func SpecFromImageInspect(raw []byte, source, ref string) (Spec, error) {
	var arr []imageInspect
	if err := json.Unmarshal(raw, &arr); err != nil {
		// Tolerate a single (non-array) object too, as the container adapters do.
		var one imageInspect
		if err2 := json.Unmarshal(raw, &one); err2 != nil {
			return Spec{}, fmt.Errorf("parse image inspect: %w", err)
		}
		arr = []imageInspect{one}
	}
	if len(arr) == 0 {
		return Spec{}, fmt.Errorf("image inspect returned no images")
	}
	img := arr[0]

	s := Spec{
		Mode:   ModeImage,
		Source: source,
		Target: imageTarget(ref, img),
		Image:  imageTarget(ref, img),
	}

	// --- user / uid ---------------------------------------------------------
	// The ONLY containment posture an image config carries. An absent USER is a
	// determinable fact about the image (it declares no user, so a container
	// started from it defaults to uid 0), not a missing field.
	u := strings.TrimSpace(img.Config.User)
	s.User = u
	switch {
	case u == "":
		s.RunAsNonRoot = No
		s.User = "0 (no USER instruction)"
	case u == "0" || strings.HasPrefix(u, "0:") || u == "root" || strings.HasPrefix(u, "root:"):
		s.RunAsNonRoot = No
	default:
		s.RunAsNonRoot = Yes
	}

	// Everything else stays at its zero value (Unknown / "") deliberately: see
	// the package comment above. Do not "fill these in" from an image inspect.
	return s, nil
}

// UnsupportedForImageRef builds the error every score-bearing output path returns
// when the target resolved to an image reference. Those paths (badges, the
// markdown/share blocks, the --min-score CI gate, SARIF, remediation) all
// presuppose a composite score, and image mode deliberately has none. They must
// fail LOUDLY with a non-zero exit rather than emit a grade or a placeholder: a
// badge that says "0/100 F" or "15/15 A" for an image is the exact wrong number
// sitting next to a real one (IRO-712).
func UnsupportedForImageRef(what, target string) error {
	return fmt.Errorf(`%s needs a containment score, and %q is an image reference, which has none.
An image carries no run-time configuration, so six of the seven graded dimensions do not exist for it.
  to score a workload:      docker run -d --name app %s   then   ironctl scan app
  to score the image build: ironctl scan --dockerfile Dockerfile
  (plain "ironctl scan %s" still reports the image USER finding and the N/A dimensions)`,
		what, target, target, target)
}

// imageTarget picks the most useful display reference: what the caller asked for,
// else the first repo tag, else a short image id.
func imageTarget(ref string, img imageInspect) string {
	if r := strings.TrimSpace(ref); r != "" {
		return r
	}
	for _, t := range img.RepoTags {
		if t = strings.TrimSpace(t); t != "" && t != "<none>:<none>" {
			return t
		}
	}
	return shortID(strings.TrimPrefix(img.ID, "sha256:"))
}
