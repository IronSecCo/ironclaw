package parity

import (
	"context"
	"encoding/json"
	"fmt"
	"reflect"
	"testing"
	"time"

	"github.com/IronSecCo/ironclaw/internal/contract"
	"github.com/IronSecCo/ironclaw/internal/host/channels"
	"github.com/IronSecCo/ironclaw/internal/host/delivery"
	"github.com/IronSecCo/ironclaw/internal/host/gateway"
	"github.com/IronSecCo/ironclaw/internal/host/queue"
	"github.com/IronSecCo/ironclaw/internal/host/registry"
	"github.com/IronSecCo/ironclaw/internal/sandbox/tools"
)

// This is the load-bearing CROSS-AGENT seam test. The capability-change wire
// format is the one place the two trees must agree byte-for-byte: the sandbox
// emits a request and the host parses, routes, and verifies it.
// Each side is unit-tested in isolation against a hand-written literal — the
// sandbox checks what it emits, the host checks what it parses — so neither catches
// the other DRIFTING. This spec closes that gap: it drives the sandbox's REAL
// emitter (tools.RequestCapabilityChangeTool -> ParseCapabilityChange ->
// SystemActionJSON, exactly as sandbox/loop forwards it) through the host's REAL
// delivery + gateway, so a format change on either side fails here.

// sandboxCapabilityWire produces the exact bytes the sandbox loop writes to the
// outbound queue when the model calls request_capability_change. It mirrors
// internal/sandbox/loop.runAgent: invoke the tool, parse the envelope it returns,
// then render it in the host's system-action wire format.
func sandboxCapabilityWire(t *testing.T, kind, payloadJSON, reason string) (wire string, payload json.RawMessage) {
	t.Helper()
	tool := tools.NewRequestCapabilityChangeTool()
	input := json.RawMessage(fmt.Sprintf(`{"kind":%q,"payload":%s,"reason":%q}`, kind, payloadJSON, reason))
	out, err := tool.Invoke(context.Background(), input)
	if err != nil {
		t.Fatalf("sandbox request_capability_change(%s) rejected a valid request: %v", kind, err)
	}
	cc, err := tools.ParseCapabilityChange(out)
	if err != nil {
		t.Fatalf("sandbox envelope did not round-trip through ParseCapabilityChange: %v", err)
	}
	w, err := cc.SystemActionJSON()
	if err != nil {
		t.Fatalf("SystemActionJSON: %v", err)
	}
	return w, cc.Payload
}

// newCapDelivery builds a host delivery wired to a gateway with the given verifier
// chain, plus the sandbox-side outbound writer over the same shared store the host
// reads. It returns the delivery, the gateway (to inspect pending changes), the
// fake channel adapter (to assert nothing leaked to a channel), and the writer a
// test uses to enqueue exactly what the sandbox would.
func newCapDelivery(t *testing.T, chain gateway.VerifierChain) (*delivery.Delivery, *gateway.Gateway, *channels.FakeAdapter, *queue.MemOutbound) {
	t.Helper()
	reg := registry.NewMemRegistry()
	mg, err := reg.GetOrCreateMessagingGroup("fake", "C1", "", true, contract.UnknownPublic)
	if err != nil {
		t.Fatalf("messaging group: %v", err)
	}
	sess, err := reg.ResolveSession("g1", mg.ID, nil, contract.SessionShared)
	if err != nil {
		t.Fatalf("resolve session: %v", err)
	}

	// One shared store: the host reads it; the test writes it as the sandbox does.
	st := queue.NewMemStore()
	hostView := queue.NewMemOutbound(st)
	sandboxWriter := queue.NewMemOutbound(st)

	channelReg := channels.NewRegistry()
	adapter := channels.NewFakeAdapter("fake")
	if err := channelReg.Register(adapter); err != nil {
		t.Fatalf("register adapter: %v", err)
	}
	gw := gateway.New(chain, gateway.NewManualApprover(), gateway.NewLogApplier(), gateway.NewMemoryStore())
	factory := func(id contract.SessionID) (contract.OutboundReader, error) {
		if id == sess.ID {
			return hostView, nil
		}
		return queue.NewMemOutbound(queue.NewMemStore()), nil
	}
	d := delivery.New(channelReg, gw, reg, factory)
	return d, gw, adapter, sandboxWriter
}

// TestCapabilityChangeWireFormatSeam asserts the behavioral contract: for every
// ChangeKind, the bytes the sandbox emits are routed by the host to a gateway
// ChangeRequest of the matching Kind whose After is the STRUCTURED payload verbatim
// (so the verifiers and the human approver see the real config, not an opaque
// blob), and the request is held pending a human — never delivered to a channel.
func TestCapabilityChangeWireFormatSeam(t *testing.T) {
	// Representative payloads per kind, matching docs/contract.md "Capability-change
	// payload conventions". The action name the sandbox emits is the ChangeKind
	// string, so the host's authorizeSystemAction must map it 1:1.
	cases := []struct {
		kind    string
		payload string
	}{
		{"persona", `{"instructions":"be terse"}`},
		{"enabled_tools", `["search","fetch"]`},
		{"packages", `{"apt":["ripgrep"],"npm":["@scope/pkg"]}`},
		{"wiring", `{"engage":"mention","pattern":"^hey"}`},
		{"permissions", `{"grant":"viewer","member":"u42"}`},
		{"mounts", `[{"source":"/srv/data"}]`},
	}
	for _, tc := range cases {
		t.Run(tc.kind, func(t *testing.T) {
			// AlwaysRequireHuman holds every change pending, giving a deterministic
			// observable (the change never auto-applies and never blocks the test).
			d, gw, adapter, w := newCapDelivery(t, gateway.VerifierChain{gateway.AlwaysRequireHuman{}})
			wire, payload := sandboxCapabilityWire(t, tc.kind, tc.payload, "needed for the task")

			if err := w.WriteMessageOut(contract.MessageOut{
				ID: contract.MessageID("cap-" + tc.kind), Seq: 1, Kind: contract.KindSystem, Content: wire,
			}); err != nil {
				t.Fatalf("enqueue outbound: %v", err)
			}
			if err := d.Poll(context.Background()); err != nil {
				t.Fatalf("delivery poll: %v", err)
			}

			if got := adapter.Delivered(); len(got) != 0 {
				t.Fatalf("a capability change must never be delivered to a channel, got %+v", got)
			}
			pending := waitCapPending(t, gw, 1)
			if pending[0].Kind != contract.ChangeKind(tc.kind) {
				t.Fatalf("routed Kind = %q, want %q (sandbox action name must map 1:1 to a ChangeKind)",
					pending[0].Kind, tc.kind)
			}
			if !jsonValueEqual(pending[0].After, payload) {
				t.Fatalf("ChangeRequest.After did not preserve the sandbox payload:\n  After  = %s\n  payload= %s",
					pending[0].After, payload)
			}
		})
	}
}

// TestSkillInstallProposalSeam drives the REAL sandbox tool emitting a skill_install
// proposal (RFC-0006) through the host's REAL delivery, with a curated resolver wired as
// in production. It proves the in-session add→approve→execute parity loop for skills: the
// sandbox NAMES a curated skill, the host RESOLVES it through the trust gate into the
// ChangePermissions bundle a human approves (held pending, never delivered to a channel),
// reusing the same applier path as the operator `ironctl skill add`.
func TestSkillInstallProposalSeam(t *testing.T) {
	d, gw, adapter, w := newCapDelivery(t, gateway.VerifierChain{gateway.AlwaysRequireHuman{}})
	// Stand in for skills.InstallChange: resolve the NAMED curated skill into the
	// resolved ChangePermissions bundle (the sandbox can never author this content).
	d.WithSkillResolver(func(skill, version string, group contract.AgentGroupID, by contract.UserID) (contract.ChangeRequest, error) {
		if skill != "curated-skill" {
			return contract.ChangeRequest{}, fmt.Errorf("skills: %s@%s not curated", skill, version)
		}
		after, _ := json.Marshal(map[string]any{"skill": skill, "version": version, "tools": []string{"web_search"}})
		return contract.ChangeRequest{Kind: contract.ChangePermissions, AgentGroupID: group, RequestedBy: by, After: after}, nil
	})

	wire, _ := sandboxCapabilityWire(t, "skill_install", `{"skill":"curated-skill","version":"1.2.0"}`, "need web search")
	if err := w.WriteMessageOut(contract.MessageOut{ID: "cap-skill", Seq: 1, Kind: contract.KindSystem, Content: wire}); err != nil {
		t.Fatalf("enqueue outbound: %v", err)
	}
	if err := d.Poll(context.Background()); err != nil {
		t.Fatalf("delivery poll: %v", err)
	}
	if got := adapter.Delivered(); len(got) != 0 {
		t.Fatalf("a skill_install proposal must never be delivered to a channel, got %+v", got)
	}
	pending := waitCapPending(t, gw, 1)
	// The resolved install rides ChangePermissions — NOT the skill_install action name —
	// so the proven skill-install applier + respawn handle it exactly as the operator path.
	if pending[0].Kind != contract.ChangePermissions {
		t.Fatalf("routed Kind = %q, want permissions (resolved skill install)", pending[0].Kind)
	}
	var got struct {
		Skill string `json:"skill"`
	}
	if err := json.Unmarshal(pending[0].After, &got); err != nil || got.Skill != "curated-skill" {
		t.Fatalf("After is not the resolved skill bundle: %s", pending[0].After)
	}
}

// TestCapabilityChangePayloadVerifiedByHost asserts the defense-in-depth seam: a
// package payload the sandbox emits is judged by the host's real PackageNameVerifier
// on the exact bytes the sandbox produced. A clean payload passes; one carrying a
// shell metacharacter is rejected. This proves the verifier reads the payload shape
// the sandbox actually sends (see docs/contract.md), not just a hand-written literal.
func TestCapabilityChangePayloadVerifiedByHost(t *testing.T) {
	v := gateway.PackageNameVerifier{}

	// Clean: well-formed apt/npm names.
	_, clean := sandboxCapabilityWire(t, "packages", `{"apt":["ripgrep"],"npm":["@scope/pkg"]}`, "tools")
	if verdict, _, err := v.Verify(context.Background(), changePackages(clean)); err != nil || verdict != contract.VerdictPass {
		t.Fatalf("clean package payload: verdict=%v err=%v, want Pass", verdict, err)
	}

	// Malicious: a shell-injection attempt smuggled into a package name. The sandbox
	// tool happily forwards it (it does not police names) — the host verifier is the
	// line of defense, and it must reject.
	_, evil := sandboxCapabilityWire(t, "packages", `{"npm":["left-pad; rm -rf /"]}`, "totally legit")
	if verdict, _, err := v.Verify(context.Background(), changePackages(evil)); err != nil || verdict != contract.VerdictReject {
		t.Fatalf("malicious package payload: verdict=%v err=%v, want Reject", verdict, err)
	}
}

// changePackages builds a ChangePackages request with After set to the payload the
// sandbox emitted — mirroring how host delivery threads the envelope's payload into
// ChangeRequest.After (proven verbatim by TestCapabilityChangeWireFormatSeam).
func changePackages(payload json.RawMessage) contract.ChangeRequest {
	return contract.ChangeRequest{Kind: contract.ChangePackages, After: payload}
}

// waitCapPending polls the gateway until it holds want pending changes (delivery
// submits in a background goroutine), failing the test on timeout.
func waitCapPending(t *testing.T, gw *gateway.Gateway, want int) []contract.ChangeRequest {
	t.Helper()
	for i := 0; i < 500; i++ {
		if p, _ := gw.Pending(); len(p) == want {
			return p
		}
		time.Sleep(time.Millisecond)
	}
	t.Fatalf("gateway did not reach %d pending change(s) within timeout", want)
	return nil
}

// jsonValueEqual reports whether two JSON byte slices encode the same value,
// ignoring key order and insignificant whitespace.
func jsonValueEqual(a, b []byte) bool {
	var av, bv interface{}
	if json.Unmarshal(a, &av) != nil || json.Unmarshal(b, &bv) != nil {
		return false
	}
	return reflect.DeepEqual(av, bv)
}
