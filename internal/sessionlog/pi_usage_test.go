package sessionlog

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

// piUsageSession is a synthetic omp-shaped transcript: a header, a user turn,
// two usage-bearing assistant messages (one priced by the harness, one with a
// zero cost), a tool execution, and a zero-token assistant entry.
const piUsageSession = `{"type":"session","version":3,"id":"ses_usage","timestamp":"2026-02-02T00:00:00.000Z","cwd":"/tmp/gascity/omp"}
{"type":"message","id":"msg_user_1","parentId":null,"timestamp":"2026-02-02T00:00:00.000Z","message":{"role":"user","content":"Do the thing.","timestamp":1770000000000}}
{"type":"message","id":"msg_a1","parentId":"msg_user_1","timestamp":"2026-02-02T00:00:01.000Z","message":{"role":"assistant","content":[{"type":"text","text":"Done."}],"provider":"deepseek","model":"deepseek-v4","usage":{"input":1200,"output":80,"cacheRead":300,"cacheWrite":40,"reasoningTokens":25,"totalTokens":1620,"cost":{"input":0.001,"output":0.0002,"cacheRead":0.00001,"cacheWrite":0.00002,"total":0.00123}},"stopReason":"stop","timestamp":1770000001000}}
{"type":"message","id":"msg_bash","parentId":"msg_a1","timestamp":"2026-02-02T00:00:02.000Z","message":{"role":"bashExecution","command":"true","output":"","exitCode":0,"timestamp":1770000002000}}
{"type":"message","id":"msg_a2","parentId":"msg_bash","timestamp":"2026-02-02T00:00:03.000Z","message":{"role":"assistant","content":[{"type":"text","text":"Again."}],"provider":"deepseek","model":"deepseek-v4","usage":{"input":50,"output":5,"cacheRead":0,"cacheWrite":0,"totalTokens":55,"cost":{"input":0,"output":0,"cacheRead":0,"cacheWrite":0,"total":0}},"stopReason":"stop","timestamp":1770000003000}}
{"type":"message","id":"msg_a3","parentId":"msg_a2","timestamp":"2026-02-02T00:00:04.000Z","message":{"role":"assistant","content":[],"provider":"deepseek","model":"deepseek-v4","usage":{"input":0,"output":0,"cacheRead":0,"cacheWrite":0,"totalTokens":0},"stopReason":"aborted","timestamp":1770000004000}}
`

func writePiUsageSession(t *testing.T, dir string) string {
	t.Helper()
	path := filepath.Join(dir, "session.jsonl")
	if err := os.WriteFile(path, []byte(piUsageSession), 0o644); err != nil {
		t.Fatal(err)
	}
	return path
}

func TestExtractPiTailUsage(t *testing.T) {
	got, err := ExtractPiTailUsage(writePiUsageSession(t, t.TempDir()))
	if err != nil {
		t.Fatalf("ExtractPiTailUsage: %v", err)
	}
	// The user turn, the tool execution and the zero-token entry are skipped.
	if len(got) != 2 {
		t.Fatalf("got %d usages, want 2: %+v", len(got), got)
	}

	first := got[0]
	if first.MessageID != "msg_a1" || first.EntryUUID != "msg_a1" {
		t.Errorf("identity = %q/%q, want msg_a1", first.MessageID, first.EntryUUID)
	}
	if first.Model != "deepseek-v4" {
		t.Errorf("Model = %q", first.Model)
	}
	if first.InputTokens != 1200 || first.OutputTokens != 80 || first.ReasoningTokens != 25 ||
		first.CacheReadTokens != 300 || first.CacheCreationTokens != 40 {
		t.Errorf("tokens = %+v", first)
	}
	if !first.HasReportedCost || first.ReportedCostUSD != 0.00123 {
		t.Errorf("reported cost = %v/%v, want 0.00123/true", first.ReportedCostUSD, first.HasReportedCost)
	}
	if want := time.UnixMilli(1770000001000); !first.Timestamp.Equal(want) {
		t.Errorf("Timestamp = %v, want %v", first.Timestamp, want)
	}

	// pi writes a zero total when its model table has no rate, so a zero is
	// not a reported cost: the price table decides.
	if got[1].MessageID != "msg_a2" || got[1].HasReportedCost {
		t.Errorf("zero-cost entry = %+v", got[1])
	}
}

func TestExtractPiTailUsageFromSearchPaths(t *testing.T) {
	root := t.TempDir()
	if _, err := ExtractPiTailUsageFromSearchPaths([]string{root}, writePiUsageSession(t, root)); err != nil {
		t.Fatalf("inside search path: %v", err)
	}
	if _, err := ExtractPiTailUsageFromSearchPaths([]string{root}, writePiUsageSession(t, t.TempDir())); err == nil {
		t.Fatal("path outside the search roots was read")
	}
}
