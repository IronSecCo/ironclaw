package scan

import (
	"encoding/json"
	"fmt"
	"testing"
)

// admObjWeak / admObjHardened are the SAME workloads as k8s_test.go's fixtures,
// expressed as JSON objects so they can be embedded verbatim inside an
// AdmissionReview request.object (the API server always sends JSON).
const admObjHardened = `{
  "apiVersion": "v1",
  "kind": "Pod",
  "metadata": {"name": "hardened-pod"},
  "spec": {
    "containers": [{
      "name": "app",
      "image": "ironclaw",
      "securityContext": {
        "runAsNonRoot": true,
        "runAsUser": 65532,
        "readOnlyRootFilesystem": true,
        "allowPrivilegeEscalation": false,
        "capabilities": {"drop": ["ALL"]},
        "seccompProfile": {"type": "RuntimeDefault"}
      }
    }]
  }
}`

const admObjWeak = `{
  "apiVersion": "v1",
  "kind": "Pod",
  "metadata": {"name": "weak-pod"},
  "spec": {
    "hostPID": true,
    "hostNetwork": true,
    "containers": [{
      "name": "app",
      "image": "nginx",
      "securityContext": {"privileged": true}
    }]
  }
}`

const admObjDeployment = `{
  "apiVersion": "apps/v1",
  "kind": "Deployment",
  "metadata": {"name": "dep"},
  "spec": {"template": {"spec": {"containers": [{
    "name": "app",
    "securityContext": {
      "runAsUser": 1000,
      "readOnlyRootFilesystem": true,
      "capabilities": {"drop": ["ALL"]},
      "seccompProfile": {"type": "RuntimeDefault"}
    }
  }]}}}
}`

// review wraps a k8s object JSON in an AdmissionReview request with the given uid.
func review(uid, obj string) string {
	return fmt.Sprintf(`{"apiVersion":"admission.k8s.io/v1","kind":"AdmissionReview","request":{"uid":%q,"operation":"CREATE","kind":{"group":"","version":"v1","kind":"Pod"},"name":"x","namespace":"default","object":%s}}`, uid, obj)
}

// TestAdmission_ParityWithK8s is the core acceptance: an admitted object grades
// IDENTICALLY to the same object fed through --k8s. The admission path is a thin
// wrapper over the SAME scorer, so every posture field and the final score must
// match; only the Source label differs.
func TestAdmission_ParityWithK8s(t *testing.T) {
	for _, obj := range []struct {
		name string
		json string
	}{
		{"hardened", admObjHardened},
		{"weak", admObjWeak},
		{"deployment", admObjDeployment},
	} {
		t.Run(obj.name, func(t *testing.T) {
			direct, err := SpecFromK8s([]byte(obj.json))
			if err != nil {
				t.Fatalf("SpecFromK8s: %v", err)
			}
			adm, uid, err := SpecFromAdmissionReview([]byte(review("uid-1", obj.json)))
			if err != nil {
				t.Fatalf("SpecFromAdmissionReview: %v", err)
			}
			if uid != "uid-1" {
				t.Errorf("uid=%q, want uid-1", uid)
			}
			if adm.Source != "k8s-admission" {
				t.Errorf("source=%q, want k8s-admission", adm.Source)
			}
			// Normalize Source so the rest of the Spec must match byte-for-byte.
			direct.Source, adm.Source = "", ""
			dj, _ := json.Marshal(direct)
			aj, _ := json.Marshal(adm)
			if string(dj) != string(aj) {
				t.Errorf("admission spec diverged from --k8s scorer:\n direct=%s\n admit =%s", dj, aj)
			}
			if Score(direct).Score != Score(adm).Score {
				t.Errorf("score parity broken: k8s=%d admission=%d", Score(direct).Score, Score(adm).Score)
			}
		})
	}
}

// TestAdmission_GradesDeleteOldObject: a DELETE review carries oldObject, not
// object; it must still be gradeable rather than a hard error.
func TestAdmission_GradesDeleteOldObject(t *testing.T) {
	raw := fmt.Sprintf(`{"apiVersion":"admission.k8s.io/v1","kind":"AdmissionReview","request":{"uid":"del-1","operation":"DELETE","oldObject":%s}}`, admObjWeak)
	s, uid, err := SpecFromAdmissionReview([]byte(raw))
	if err != nil {
		t.Fatalf("delete review should grade oldObject: %v", err)
	}
	if uid != "del-1" || s.Privileged != Yes {
		t.Errorf("oldObject not graded: uid=%q spec=%+v", uid, s)
	}
}

// TestAdmission_FailClosed: an enforcement gate must ERROR (deny) on any input it
// cannot grade, never silently allow.
func TestAdmission_FailClosed(t *testing.T) {
	cases := map[string]string{
		"garbage":        `not json at all`,
		"wrong kind":     `{"kind":"Pod"}`,
		"no request":     `{"apiVersion":"admission.k8s.io/v1","kind":"AdmissionReview"}`,
		"no object":      `{"kind":"AdmissionReview","request":{"uid":"u"}}`,
		"empty object":   `{"kind":"AdmissionReview","request":{"uid":"u","object":{}}}`,
		"object no ctrs": `{"kind":"AdmissionReview","request":{"uid":"u","object":{"kind":"Pod","spec":{}}}}`,
	}
	for name, raw := range cases {
		t.Run(name, func(t *testing.T) {
			if _, _, err := SpecFromAdmissionReview([]byte(raw)); err == nil {
				t.Errorf("expected fail-closed error for %q input", name)
			}
		})
	}
}

// TestAdmissionReviewResponse checks the webhook response contract: uid echoed,
// allow carries no 403, deny carries a 403 status message.
func TestAdmissionReviewResponse(t *testing.T) {
	allow, err := AdmissionReviewResponse("uid-9", true, "ok", nil)
	if err != nil {
		t.Fatal(err)
	}
	var av admissionReview
	if err := json.Unmarshal(allow, &av); err != nil {
		t.Fatal(err)
	}
	if av.APIVersion != "admission.k8s.io/v1" || av.Kind != "AdmissionReview" {
		t.Errorf("bad envelope: %+v", av)
	}
	if av.Response == nil || av.Response.UID != "uid-9" || !av.Response.Allowed {
		t.Errorf("allow response wrong: %+v", av.Response)
	}

	deny, err := AdmissionReviewResponse("uid-9", false, "score 10/100 below 80", nil)
	if err != nil {
		t.Fatal(err)
	}
	var dv admissionReview
	if err := json.Unmarshal(deny, &dv); err != nil {
		t.Fatal(err)
	}
	if dv.Response == nil || dv.Response.Allowed {
		t.Fatalf("deny response should not be allowed: %+v", dv.Response)
	}
	if dv.Response.UID != "uid-9" {
		t.Errorf("uid not echoed on deny: %q", dv.Response.UID)
	}
	if dv.Response.Status == nil || dv.Response.Status.Code != 403 {
		t.Errorf("deny should carry a 403 status: %+v", dv.Response.Status)
	}
}
