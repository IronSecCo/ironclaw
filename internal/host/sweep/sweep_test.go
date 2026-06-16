// OWNER: AGENT1

package sweep

import "testing"

func TestDecideStuckAction(t *testing.T) {
	tests := []struct {
		name      string
		heartbeat int64
		claim     int64
		want      StuckAction
	}{
		{name: "healthy", heartbeat: 1000, claim: 500, want: None},
		{name: "busy but heart-beating", heartbeat: 1000, claim: ClaimStaleMs + 10_000, want: None},
		{name: "heartbeat past ceiling", heartbeat: HeartbeatCeilingMs + 1, claim: 0, want: KillCeiling},
		{name: "ceiling wins over claim", heartbeat: HeartbeatCeilingMs + 1, claim: ClaimStaleMs + 1, want: KillCeiling},
		{name: "stuck claim with stale heartbeat", heartbeat: HeartbeatStaleMs + 1, claim: ClaimStaleMs + 1, want: KillClaim},
		{name: "stale claim but fresh heartbeat", heartbeat: 100, claim: ClaimStaleMs + 1, want: None},
		{name: "stale heartbeat but fresh claim", heartbeat: HeartbeatStaleMs + 1, claim: 100, want: None},
		{name: "unknown ages", heartbeat: -1, claim: -1, want: None},
		{name: "exactly at ceiling not over", heartbeat: HeartbeatCeilingMs, claim: 0, want: None},
		{name: "exactly at claim stale not over", heartbeat: HeartbeatStaleMs + 1, claim: ClaimStaleMs, want: None},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := DecideStuckAction(tt.heartbeat, tt.claim); got != tt.want {
				t.Fatalf("DecideStuckAction(%d,%d) = %v, want %v", tt.heartbeat, tt.claim, got, tt.want)
			}
		})
	}
}
