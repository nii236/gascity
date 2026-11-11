package herdr

import (
	"errors"
	"testing"

	"github.com/gastownhall/gascity/internal/runtime"
)

// TestAgentAliveFromStatus pins the core of the claude-2.1.x singleton
// restart-loop fix: liveness is derived from herdr's own agent_status, so an
// active status (idle/working/done/…) reads alive without any dependency on
// matching a process name or reading GC_SESSION_ID out of the agent's
// environment. Only an explicit terminal status reads dead, and unknown/empty
// statuses fail safe toward alive (a live singleton misread as dead is the
// destructive direction this fix exists to prevent).
func TestAgentAliveFromStatus(t *testing.T) {
	alive := []string{"idle", "working", "done", "running", "Idle", "  WORKING ", "thinking", "busy", "", "  "}
	for _, s := range alive {
		if !agentAliveFromStatus(s) {
			t.Errorf("agentAliveFromStatus(%q) = false; want true (active/unknown status must read alive)", s)
		}
	}
	dead := []string{"exited", "stopped", "dead", "gone", "terminated", "closed", "crashed", "EXITED", " Stopped "}
	for _, s := range dead {
		if agentAliveFromStatus(s) {
			t.Errorf("agentAliveFromStatus(%q) = true; want false (terminal status must read dead)", s)
		}
	}
}

// TestLivenessFromAgentAbsentOrError is the required negative case: a failed
// `agent get` or an agent herdr reports absent must be neither running nor
// alive, so a genuinely-gone session is still eligible for restart.
func TestLivenessFromAgentAbsentOrError(t *testing.T) {
	if got := livenessFromAgent(agentInfo{}, false, nil); got.Running || got.Alive {
		t.Errorf("absent agent: got %+v; want Running=false Alive=false", got)
	}
	if got := livenessFromAgent(agentInfo{AgentStatus: "idle"}, false, errors.New("herdr transport failure")); got.Running || got.Alive {
		t.Errorf("query error: got %+v; want Running=false Alive=false", got)
	}
}

// TestLivenessFromAgentPresent covers the incident case: a present, idle
// singleton mayor must report Running=true and Alive=true (pre-fix it reported
// Alive=false via the process-table walk and looped). A present agent herdr
// reports terminal reads running-but-dead so the reconciler can restart it.
func TestLivenessFromAgentPresent(t *testing.T) {
	live := livenessFromAgent(agentInfo{Name: "mayor", AgentStatus: "idle"}, true, nil)
	if !live.Running || !live.Alive {
		t.Errorf("present idle mayor: got %+v; want Running=true Alive=true", live)
	}
	terminal := livenessFromAgent(agentInfo{Name: "mayor", AgentStatus: "exited"}, true, nil)
	if !terminal.Running || terminal.Alive {
		t.Errorf("present exited agent: got %+v; want Running=true Alive=false", terminal)
	}
}

// ListRunning is not complete here: a bound session whose pane binding does
// not resolve is omitted with no error. An error-free listing is therefore no
// proof of absence, and the provider must stay unattested until its listing
// reports that gap.
// Kills: a listing attestation declared while ListRunning still omits live
// sessions.
func TestListRunningIsNotAttested(t *testing.T) {
	for _, sp := range []any{(*Provider)(nil)} {
		if _, ok := sp.(runtime.ListingAttestation); ok {
			t.Errorf("%T declares runtime.ListingAttestation", sp)
		}
	}
}

// Kills a reported idle boundary that reads anything but herdr's own
// agent_status: idle and done are at the boundary, working is busy, blocked
// is pending, and a failed query, an absent agent or a status herdr could
// not classify read unknown, so the caller never types on a guess.
func TestIdleBoundaryFromAgent(t *testing.T) {
	for _, c := range []struct {
		status  string
		present bool
		err     error
		want    runtime.IdleBoundary
	}{
		{status: "idle", present: true, want: runtime.IdleBoundaryAt},
		{status: " Done ", present: true, want: runtime.IdleBoundaryAt},
		{status: "working", present: true, want: runtime.IdleBoundaryBusy},
		{status: "blocked", present: true, want: runtime.IdleBoundaryPending},
		{status: "unknown", present: true, want: runtime.IdleBoundaryUnknown},
		{status: "", present: true, want: runtime.IdleBoundaryUnknown},
		{status: "idle", present: false, want: runtime.IdleBoundaryUnknown},
		{status: "idle", present: true, err: errors.New("herdr transport failure"), want: runtime.IdleBoundaryUnknown},
	} {
		if got := idleBoundaryFromAgent(agentInfo{AgentStatus: c.status}, c.present, c.err); got != c.want {
			t.Errorf("idleBoundaryFromAgent(%q, present=%t, err=%v) = %s, want %s", c.status, c.present, c.err, got, c.want)
		}
	}
}
