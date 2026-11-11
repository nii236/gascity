package session

import (
	"context"
	"fmt"
	"testing"

	"github.com/gastownhall/gascity/internal/beads"
	"github.com/gastownhall/gascity/internal/beads/beadstest"
	"github.com/gastownhall/gascity/internal/runtime"
)

// idleSnapshotFake is a Fake whose one-look idle snapshot answers idle and
// err (runtime.IdleSnapshotProvider).
type idleSnapshotFake struct {
	*runtime.Fake
	idle bool
	err  error
}

func (f *idleSnapshotFake) SnapshotIdle(string) (bool, error) { return f.idle, f.err }

// Both wait-idle nudges take a pre-read first (runtime.IdleBoundaryNow): a
// session that reads busy, or reads indeterminate while its runtime reports a
// pending interaction (an open dialog), is reported undelivered at once, for
// its caller to queue, without entering the idle wait and without typing. A
// pre-read at the boundary, or one that cannot tell, waits for idle and
// delivers as before.
func TestChatWaitIdleBusySnapshotQueuesWithoutWaiting(t *testing.T) {
	indeterminate := fmt.Errorf("scan: %w", runtime.ErrIdleIndeterminate)
	for _, tc := range []struct {
		name      string
		idle      bool
		err       error
		pending   *runtime.PendingInteraction
		delivered bool
	}{
		{name: "busy"},
		{name: "pending", err: indeterminate, pending: &runtime.PendingInteraction{RequestID: "tmux-1", Kind: "approval", Prompt: "Allow Bash?"}},
		{name: "at the boundary", idle: true, delivered: true},
		{name: "indeterminate", err: indeterminate, delivered: true},
	} {
		for path, deliver := range map[string]func(*Manager, Info, runtime.Config) (bool, error){
			"TryWaitIdleNudge": func(m *Manager, i Info, h runtime.Config) (bool, error) {
				return m.TryWaitIdleNudge(context.Background(), testActor(m, ActorBackground), i.ID, "mail", "check deploy status", BuildResumeCommand(i), h)
			},
			"TryWaitIdleNudgeLiveOnly": func(m *Manager, i Info, h runtime.Config) (bool, error) {
				return m.TryWaitIdleNudgeLiveOnly(context.Background(), i.ID, "mail", "check deploy status", h.ProcessNames)
			},
		} {
			t.Run(path+"/"+tc.name, func(t *testing.T) {
				fake := runtime.NewFake()
				sp := &idleSnapshotFake{Fake: fake, idle: tc.idle, err: tc.err}
				mgr := NewManagerWithOptions(beadstest.Sessions(beads.NewMemStore()), sp, WithCityPath(t.TempDir()))
				info, err := mgr.CreateSession(context.Background(), CreateOptions{
					Template: "helper", Command: "claude", WorkDir: t.TempDir(), Provider: "claude",
					ExtraMeta: map[string]string{"session_origin": "manual"},
				})
				if err != nil {
					t.Fatalf("CreateSession: %v", err)
				}
				fake.WaitForIdleErrors[info.SessionName] = nil
				fake.SetPendingInteraction(info.SessionName, tc.pending)
				before := len(fake.SnapshotCalls())

				delivered, err := deliver(mgr, info, runtime.Config{WorkDir: info.WorkDir})
				if err != nil {
					t.Fatalf("%s: %v", path, err)
				}
				if delivered != tc.delivered {
					t.Fatalf("%s delivered = %t, want %t", path, delivered, tc.delivered)
				}
				var waited, typed bool
				for _, call := range fake.SnapshotCalls()[before:] {
					if call.Name != info.SessionName {
						continue
					}
					switch call.Method {
					case "WaitForIdle":
						waited = true
					case "Nudge", "NudgeNow":
						typed = true
					}
				}
				if waited != tc.delivered {
					t.Fatalf("waited for idle = %t, want %t", waited, tc.delivered)
				}
				if typed != tc.delivered {
					t.Fatalf("typed = %t, want %t", typed, tc.delivered)
				}
			})
		}
	}
}

// reportedBoundaryFake is a Fake whose runtime reads the agent program's own
// reported idle boundary (runtime.ReportedIdleBoundaryProvider) as boundary.
type reportedBoundaryFake struct {
	*runtime.Fake
	boundary runtime.IdleBoundary
}

func (f *reportedBoundaryFake) ReportedIdleBoundary(string) runtime.IdleBoundary { return f.boundary }

// For a provider gc has no screen idle signature for, both wait-idle nudges
// deliver exactly when the runtime reads the program's own report at the
// input boundary: typed at once, without the screen-scan wait. A reported
// running turn, a pending dialog, or no report leaves it undelivered for
// its caller to queue, typing nothing. A claude session keeps the
// screen-scan wait whatever the program reports.
func TestChatWaitIdleReportedBoundary(t *testing.T) {
	for _, tc := range []struct {
		provider  string
		boundary  runtime.IdleBoundary
		delivered bool
		waited    bool
	}{
		{provider: "omp", boundary: runtime.IdleBoundaryAt, delivered: true},
		{provider: "omp", boundary: runtime.IdleBoundaryBusy},
		{provider: "omp", boundary: runtime.IdleBoundaryPending},
		{provider: "omp", boundary: runtime.IdleBoundaryUnknown},
		{provider: "claude", boundary: runtime.IdleBoundaryBusy, delivered: true, waited: true},
	} {
		for path, deliver := range map[string]func(*Manager, Info, runtime.Config) (bool, error){
			"TryWaitIdleNudge": func(m *Manager, i Info, h runtime.Config) (bool, error) {
				return m.TryWaitIdleNudge(context.Background(), testActor(m, ActorBackground), i.ID, "mail", "check deploy status", BuildResumeCommand(i), h)
			},
			"TryWaitIdleNudgeLiveOnly": func(m *Manager, i Info, h runtime.Config) (bool, error) {
				return m.TryWaitIdleNudgeLiveOnly(context.Background(), i.ID, "mail", "check deploy status", h.ProcessNames)
			},
		} {
			t.Run(path+"/"+tc.provider+"/"+tc.boundary.String(), func(t *testing.T) {
				fake := runtime.NewFake()
				sp := &reportedBoundaryFake{Fake: fake, boundary: tc.boundary}
				mgr := NewManagerWithOptions(beadstest.Sessions(beads.NewMemStore()), sp, WithCityPath(t.TempDir()))
				info, err := mgr.CreateSession(context.Background(), CreateOptions{
					Template: "helper", Command: tc.provider, WorkDir: t.TempDir(), Provider: tc.provider,
					ExtraMeta: map[string]string{"session_origin": "manual"},
				})
				if err != nil {
					t.Fatalf("CreateSession: %v", err)
				}
				fake.WaitForIdleErrors[info.SessionName] = nil
				before := len(fake.SnapshotCalls())

				delivered, err := deliver(mgr, info, runtime.Config{WorkDir: info.WorkDir})
				if err != nil {
					t.Fatalf("%s: %v", path, err)
				}
				if delivered != tc.delivered {
					t.Fatalf("%s delivered = %t, want %t", path, delivered, tc.delivered)
				}
				var waited, typed bool
				for _, call := range fake.SnapshotCalls()[before:] {
					if call.Name != info.SessionName {
						continue
					}
					switch call.Method {
					case "WaitForIdle":
						waited = true
					case "Nudge", "NudgeNow":
						typed = true
					}
				}
				if waited != tc.waited {
					t.Fatalf("waited for idle = %t, want %t", waited, tc.waited)
				}
				if typed != tc.delivered {
					t.Fatalf("typed = %t, want %t", typed, tc.delivered)
				}
			})
		}
	}
}
