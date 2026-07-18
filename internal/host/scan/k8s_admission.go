package scan

import (
	"encoding/json"
	"fmt"
	"strings"
)

// admissionReview is the minimal shape of a Kubernetes admission.k8s.io/v1
// AdmissionReview REQUEST: enough to pull the admitted object and echo the uid.
// It is decoded with encoding/json (the API server always sends JSON); the
// carried object is then handed to the SAME pod-spec scorer as `--k8s`.
type admissionReview struct {
	APIVersion string             `json:"apiVersion"`
	Kind       string             `json:"kind"`
	Request    *admissionRequest  `json:"request,omitempty"`
	Response   *admissionResponse `json:"response,omitempty"`
}

// admissionRequest mirrors the request half of an AdmissionReview. Object is the
// resource being admitted (the CREATE/UPDATE target); OldObject is the prior
// state (populated on UPDATE/DELETE). Both are kept raw so the graded object is
// fed verbatim to SpecFromK8s.
type admissionRequest struct {
	UID       string          `json:"uid"`
	Kind      groupVersionK   `json:"kind"`
	Name      string          `json:"name"`
	Namespace string          `json:"namespace"`
	Operation string          `json:"operation"`
	Object    json.RawMessage `json:"object"`
	OldObject json.RawMessage `json:"oldObject"`
}

// groupVersionK is the request.kind GroupVersionKind (used only for diagnostics).
type groupVersionK struct {
	Group   string `json:"group"`
	Version string `json:"version"`
	Kind    string `json:"kind"`
}

// admissionResponse is the response half IronClaw emits when acting as a webhook
// backend. status carries a human-readable deny reason (surfaced by kubectl when
// the API server rejects the object).
type admissionResponse struct {
	UID      string           `json:"uid"`
	Allowed  bool             `json:"allowed"`
	Status   *admissionStatus `json:"status,omitempty"`
	Warnings []string         `json:"warnings,omitempty"`
	// PatchType/Patch are intentionally omitted: this is a VALIDATING gate, never
	// mutating. It only ever allows or denies; it never rewrites the object.
}

type admissionStatus struct {
	Code    int    `json:"code,omitempty"`
	Message string `json:"message"`
}

// SpecFromAdmissionReview parses a Kubernetes admission.k8s.io/v1 AdmissionReview
// request payload (the JSON body a ValidatingWebhookConfiguration POSTs to its
// backend) and grades the admitted workload with the SAME pod-spec scorer as
// `--k8s`. It returns the graded Spec and the request uid (which a webhook
// response MUST echo). The source is labeled "k8s-admission" so the scorecard
// names the origin.
//
// Fail-CLOSED, and deliberately unlike the fail-OPEN batch modes (--helm /
// --terraform): an admission webhook is an ENFORCEMENT gate, so an unparseable
// review, a missing request, or an object with nothing to grade is an ERROR the
// caller must translate into a DENY — never a silent allow. The one tolerated
// no-object case is a sub-resource/connect request the caller handles explicitly.
func SpecFromAdmissionReview(raw []byte) (Spec, string, error) {
	var ar admissionReview
	if err := json.Unmarshal(raw, &ar); err != nil {
		return Spec{}, "", fmt.Errorf("parse AdmissionReview: %w", err)
	}
	if k := strings.TrimSpace(ar.Kind); k != "" && k != "AdmissionReview" {
		return Spec{}, "", fmt.Errorf("payload kind is %q, not AdmissionReview", k)
	}
	if ar.Request == nil {
		return Spec{}, "", fmt.Errorf("AdmissionReview carries no request")
	}
	uid := ar.Request.UID

	// Grade the object under admission. On DELETE the object is empty and only
	// oldObject is present; grade whichever is available so a delete review is
	// still gradeable rather than a hard error.
	obj := ar.Request.Object
	if len(obj) == 0 {
		obj = ar.Request.OldObject
	}
	if len(obj) == 0 {
		return Spec{}, uid, fmt.Errorf("admission request for %s carries no object to grade", nz(admissionKindLabel(ar.Request), "workload"))
	}

	spec, err := SpecFromK8s(obj)
	if err != nil {
		return Spec{}, uid, err
	}
	spec.Source = "k8s-admission"
	return spec, uid, nil
}

// admissionKindLabel renders a request's GroupVersionKind + name for diagnostics
// (e.g. "apps/v1 Deployment web").
func admissionKindLabel(req *admissionRequest) string {
	if req == nil {
		return ""
	}
	gv := strings.TrimPrefix(req.Kind.Group+"/"+req.Kind.Version, "/")
	kind := strings.TrimSpace(req.Kind.Kind)
	label := strings.TrimSpace(gv + " " + kind)
	if n := strings.TrimSpace(req.Name); n != "" {
		label = strings.TrimSpace(label + " " + n)
	}
	return strings.TrimSpace(label)
}

// AdmissionReviewResponse builds the admission.k8s.io/v1 AdmissionReview RESPONSE
// a webhook backend returns to the API server. uid MUST match the request uid
// (the API server rejects a mismatch). allowed decides admission; when denied,
// message is surfaced to the user by kubectl (HTTP 403). warnings are non-fatal
// notes shown regardless of the decision. The bytes are ready to write to the
// webhook's HTTP response body.
func AdmissionReviewResponse(uid string, allowed bool, message string, warnings []string) ([]byte, error) {
	resp := admissionReview{
		APIVersion: "admission.k8s.io/v1",
		Kind:       "AdmissionReview",
		Response: &admissionResponse{
			UID:      uid,
			Allowed:  allowed,
			Warnings: warnings,
		},
	}
	if !allowed {
		resp.Response.Status = &admissionStatus{Code: 403, Message: nz(message, "denied by IronClaw containment gate")}
	} else if strings.TrimSpace(message) != "" {
		// An allowed response may still carry an informational status message.
		resp.Response.Status = &admissionStatus{Code: 200, Message: message}
	}
	out, err := json.Marshal(resp)
	if err != nil {
		return nil, fmt.Errorf("marshal AdmissionReview response: %w", err)
	}
	return out, nil
}
