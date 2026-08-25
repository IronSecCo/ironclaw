// FROZEN CONTRACT — do not edit without a joint RFC (see docs/contract.md).

package contract

import "testing"

func TestStringEnumValues(t *testing.T) {
	tests := []struct {
		name string
		got  string
		want string
	}{
		{name: "KindChat", got: string(KindChat), want: "chat"},
		{name: "KindTask", got: string(KindTask), want: "task"},
		{name: "KindWebhook", got: string(KindWebhook), want: "webhook"},
		{name: "KindSystem", got: string(KindSystem), want: "system"},
		{name: "EngagePattern", got: string(EngagePattern), want: "pattern"},
		{name: "EngageMention", got: string(EngageMention), want: "mention"},
		{name: "EngageMentionSticky", got: string(EngageMentionSticky), want: "mention-sticky"},
		{name: "SenderAll", got: string(SenderAll), want: "all"},
		{name: "SenderKnown", got: string(SenderKnown), want: "known"},
		{name: "IgnoreDrop", got: string(IgnoreDrop), want: "drop"},
		{name: "IgnoreAccumulate", got: string(IgnoreAccumulate), want: "accumulate"},
		{name: "UnknownStrict", got: string(UnknownStrict), want: "strict"},
		{name: "UnknownRequestApproval", got: string(UnknownRequestApproval), want: "request_approval"},
		{name: "UnknownPublic", got: string(UnknownPublic), want: "public"},
		{name: "SessionShared", got: string(SessionShared), want: "shared"},
		{name: "SessionPerThread", got: string(SessionPerThread), want: "per-thread"},
		{name: "SessionAgentShared", got: string(SessionAgentShared), want: "agent-shared"},
		{name: "ChangePersona", got: string(ChangePersona), want: "persona"},
		{name: "ChangeEnabledTools", got: string(ChangeEnabledTools), want: "enabled_tools"},
		{name: "ChangePackages", got: string(ChangePackages), want: "packages"},
		{name: "ChangeWiring", got: string(ChangeWiring), want: "wiring"},
		{name: "ChangePermissions", got: string(ChangePermissions), want: "permissions"},
		{name: "ChangeMounts", got: string(ChangeMounts), want: "mounts"},
		{name: "ChangeCreateAgent", got: string(ChangeCreateAgent), want: "create_agent"},
		{name: "ChangeMCPAccess", got: string(ChangeMCPAccess), want: "mcp_access"},
		{name: "ChangeSkillInstall", got: string(ChangeSkillInstall), want: "skill_install"},
		{name: "ChangeMCPRegister", got: string(ChangeMCPRegister), want: "mcp_register"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if tt.got != tt.want {
				t.Fatalf("value = %q, want %q", tt.got, tt.want)
			}
		})
	}
}

func TestVerdictValues(t *testing.T) {
	tests := []struct {
		name string
		got  Verdict
		want int
	}{
		{name: "VerdictPass", got: VerdictPass, want: 0},
		{name: "VerdictReject", got: VerdictReject, want: 1},
		{name: "VerdictRequireHuman", got: VerdictRequireHuman, want: 2},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if int(tt.got) != tt.want {
				t.Fatalf("value = %d, want %d", tt.got, tt.want)
			}
		})
	}
}
