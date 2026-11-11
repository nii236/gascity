package worker

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/gastownhall/gascity/internal/beads"
	"github.com/gastownhall/gascity/internal/pricing"
	"github.com/gastownhall/gascity/internal/runtime"
	sessionpkg "github.com/gastownhall/gascity/internal/session"
	"github.com/gastownhall/gascity/internal/usage"
)

func TestInvocationUsageFamilyPi(t *testing.T) {
	for _, provider := range []string{"pi", "omp", "pi/tmux-cli", "omp/tmux-cli"} {
		family, ok := InvocationUsageFamily(provider)
		if family != "pi" || !ok {
			t.Errorf("InvocationUsageFamily(%q) = %q, %v; want pi, true", provider, family, ok)
		}
	}
}

// TestFactorySweepSessionModelUsageAtPathOMP sweeps a synthetic omp transcript
// and checks the emitted model facts: a positive harness-reported cost is
// used as is, and a zero one is priced from the city's [[pricing]] entry.
func TestFactorySweepSessionModelUsageAtPathOMP(t *testing.T) {
	searchBase := t.TempDir()
	sinkPath := filepath.Join(t.TempDir(), "usage.jsonl")
	registry := pricing.BuildRegistry(nil, []pricing.ModelPricing{{
		Provider:     "pi",
		Model:        "deepseek-v4",
		Tier:         pricing.Tier{PromptUSDPer1M: 1, CompletionUSDPer1M: 2},
		LastVerified: "2026-01-01",
	}})

	store := beads.NewMemStore()
	factory, err := NewFactory(FactoryConfig{
		CityPath:    t.TempDir(),
		Store:       store,
		Provider:    runtime.NewFake(),
		SearchPaths: []string{searchBase},
		UsageSink:   usage.NewLocalSink(sinkPath),
		Pricing:     registry,
	})
	if err != nil {
		t.Fatalf("NewFactory: %v", err)
	}
	h, err := factory.Session(SessionSpec{
		Profile:  ProfilePiTmuxCLI,
		Template: "probe",
		Title:    "Probe omp",
		Command:  "omp",
		WorkDir:  t.TempDir(),
		Provider: "omp",
		Metadata: map[string]string{"agent_name": "myrig/omp"},
	})
	if err != nil {
		t.Fatalf("Session: %v", err)
	}
	if err := h.Start(context.Background(), handleActor(t, h, sessionpkg.ActorAgent)); err != nil {
		t.Fatalf("Start: %v", err)
	}
	b, err := store.Get(h.sessionID)
	if err != nil {
		t.Fatal(err)
	}

	path := filepath.Join(searchBase, "session.jsonl")
	body := `{"type":"session","version":3,"id":"ses_omp","timestamp":"2026-02-02T00:00:00.000Z","cwd":"/tmp/gascity/omp"}
{"type":"message","id":"msg_u1","parentId":null,"timestamp":"2026-02-02T00:00:00.000Z","message":{"role":"user","content":"Go.","timestamp":1770000000000}}
{"type":"message","id":"msg_a1","parentId":"msg_u1","timestamp":"2026-02-02T00:00:01.000Z","message":{"role":"assistant","content":[],"provider":"deepseek","model":"deepseek-v4","usage":{"input":1000,"output":10,"cacheRead":5,"cacheWrite":0,"reasoningTokens":3,"totalTokens":1015,"cost":{"total":0.5}},"stopReason":"stop","timestamp":1770000001000}}
{"type":"message","id":"msg_a2","parentId":"msg_a1","timestamp":"2026-02-02T00:00:02.000Z","message":{"role":"assistant","content":[],"provider":"deepseek","model":"deepseek-v4","usage":{"input":1000000,"output":1000000,"cacheRead":0,"cacheWrite":0,"totalTokens":2000000,"cost":{"total":0}},"stopReason":"stop","timestamp":1770000002000}}
`
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}

	now := time.Unix(1, 0).UTC()
	if emitted, settled, err := factory.SweepSessionModelUsageAtPath(context.Background(), h.sessionID, b.Metadata, path, now); err != nil || !settled || emitted != 2 {
		t.Fatalf("sweep = %d, %v, %v; want 2, true, nil", emitted, settled, err)
	}
	facts, warnings, err := usage.ReadFacts(sinkPath)
	if err != nil || len(warnings) != 0 {
		t.Fatalf("ReadFacts: %v %v", err, warnings)
	}
	byReq := map[string]usage.Fact{}
	for _, f := range facts {
		byReq[f.UpstreamReqID] = f
	}
	if f := byReq["msg_a1"]; f.Kind != usage.KindModel || f.Provider != "pi" || f.Model != "deepseek-v4" ||
		f.Unpriced || f.CostUSDEstimate != 0.5 || f.InputTokens != 1000 || f.OutputTokens != 10 || f.CacheReadTokens != 5 {
		t.Errorf("msg_a1 = %+v, want a pi model fact priced from the reported cost 0.5", f)
	}
	if f := byReq["msg_a2"]; f.Unpriced || f.CostUSDEstimate != 3 {
		t.Errorf("msg_a2 = %+v, want priced 3 from the city pricing entry", f)
	}
}
