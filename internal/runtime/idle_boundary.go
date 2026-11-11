package runtime

import "errors"

// IdleBoundary is one non-blocking reading of whether a session is at an
// idle input boundary now ([IdleBoundaryNow]).
type IdleBoundary uint8

const (
	// IdleBoundaryUnknown means the reading says neither: the runtime takes
	// no idle snapshot of the session, the snapshot failed, or it was
	// indeterminate, saw no pending choice, and the runtime reported no
	// pending interaction. Callers keep the rule they would use without it.
	IdleBoundaryUnknown IdleBoundary = iota
	// IdleBoundaryAt means the session reads idle at its input boundary.
	IdleBoundaryAt
	// IdleBoundaryBusy means the runtime reports the session busy, a reading
	// the runtime trusts for the session's provider family.
	IdleBoundaryBusy
	// IdleBoundaryPending means a pending interaction or choice owns the
	// session's input: text sent now would answer it.
	IdleBoundaryPending
)

// String names the reading.
func (b IdleBoundary) String() string {
	switch b {
	case IdleBoundaryAt:
		return "at"
	case IdleBoundaryBusy:
		return "busy"
	case IdleBoundaryPending:
		return "pending"
	default:
		return "unknown"
	}
}

// OutOfReach reports whether the reading rules out an idle boundary within a
// short wait: the session is busy, or a pending interaction owns its input.
func (b IdleBoundary) OutOfReach() bool {
	return b == IdleBoundaryBusy || b == IdleBoundaryPending
}

// IdleBoundaryNow takes a single [IdleSnapshotProvider] reading of session
// name on p, without waiting, and classifies it. It is the pre-read of a
// wait-idle delivery: a session that reads busy or pending will not reach an
// idle boundary within a short wait, so the delivery is queued at once
// instead. Which provider families' busy readings count is the runtime's
// policy: a runtime that does not trust a family's busy reading reports it
// as [ErrIdleIndeterminate]. An open permission prompt or question dialog
// reads indeterminate too, showing neither a ready prompt nor a busy
// indicator, so an indeterminate reading is pending when it saw a menu
// waiting for a choice ([ErrIdlePendingChoice]), or when the runtime reports
// a pending interaction for the session ([InteractionProvider]); any other
// indeterminate reading is [IdleBoundaryUnknown].
func IdleBoundaryNow(p Provider, name string) IdleBoundary {
	snap, ok := p.(IdleSnapshotProvider)
	if !ok {
		return IdleBoundaryUnknown
	}
	idle, err := snap.SnapshotIdle(name)
	return idleBoundaryOf(p, name, idle, err)
}

// idleBoundaryOf classifies one idle snapshot of session name on p (idle,
// err), as [IdleBoundaryNow] documents.
func idleBoundaryOf(p Provider, name string, idle bool, err error) IdleBoundary {
	switch {
	case errors.Is(err, ErrIdlePendingChoice):
		return IdleBoundaryPending
	case errors.Is(err, ErrIdleIndeterminate):
		if interactionPending(p, name) {
			return IdleBoundaryPending
		}
		return IdleBoundaryUnknown
	case err != nil:
		return IdleBoundaryUnknown
	case idle:
		return IdleBoundaryAt
	}
	return IdleBoundaryBusy
}

// interactionPending reports whether p reports a pending interaction for
// session name. A runtime without [InteractionProvider], or whose look
// fails, reports none: the pre-read then cannot tell, and its caller waits.
func interactionPending(p Provider, name string) bool {
	ip, ok := p.(InteractionProvider)
	if !ok {
		return false
	}
	pending, err := ip.Pending(name)
	return err == nil && pending != nil
}

// ReportedIdleBoundaryProvider is an optional extension for runtimes that
// read whether a session is at its input boundary from the agent program's
// own report of its state (an OSC 7501 program-status record, a terminal
// multiplexer's agent status), not from a screen scan calibrated to one
// provider family's TUI. The reading is one fresh look, never a cached
// snapshot: [IdleBoundaryAt] only when the report says the program is at its
// input boundary, [IdleBoundaryBusy] for a running turn,
// [IdleBoundaryPending] for a dialog that owns the input, and
// [IdleBoundaryUnknown] whenever the runtime has no report it may decide
// with. Composites do not implement it; it is read on the routed leaf
// ([ReportedIdleBoundaryNow]).
type ReportedIdleBoundaryProvider interface {
	ReportedIdleBoundary(name string) IdleBoundary
}

// ReportedIdleBoundaryNow reads session name's reported idle boundary on the
// leaf serving it ([ReportedIdleBoundaryProvider]). A leaf without the
// capability reads unknown.
func ReportedIdleBoundaryNow(p Provider, name string) IdleBoundary {
	if p == nil || name == "" {
		return IdleBoundaryUnknown
	}
	leaf, _, _ := ResolveBackend(p, name)
	r, ok := leaf.(ReportedIdleBoundaryProvider)
	if !ok {
		return IdleBoundaryUnknown
	}
	return r.ReportedIdleBoundary(name)
}
