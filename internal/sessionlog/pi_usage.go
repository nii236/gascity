package sessionlog

import (
	"bytes"
	"encoding/json"
	"os"
	"strings"
)

// piUsageEntry is the usage-bearing slice of one pi/omp transcript line. Only
// assistant messages carry model and usage fields.
type piUsageEntry struct {
	Type      string `json:"type"`
	ID        string `json:"id"`
	Timestamp string `json:"timestamp"`
	Message   struct {
		Role      string `json:"role"`
		Model     string `json:"model"`
		Timestamp int64  `json:"timestamp"`
		Usage     *struct {
			Input           int `json:"input"`
			Output          int `json:"output"`
			CacheRead       int `json:"cacheRead"`
			CacheWrite      int `json:"cacheWrite"`
			ReasoningTokens int `json:"reasoningTokens"`
			Cost            struct {
				Total float64 `json:"total"`
			} `json:"cost"`
		} `json:"usage"`
	} `json:"message"`
}

// ExtractPiTailUsage reads per-invocation token usage from a pi or omp session
// transcript and returns one TailUsage per usage-bearing assistant message, in
// file order. The entry id is the invocation identity callers filter by with
// their persisted cursor. pi's input count excludes cached prompt tokens, as
// TailUsage expects. pi prices each call from its own model table, so a positive
// cost.total is carried as ReportedCostUSD; a zero total is what pi writes for a
// model it has no rate for, so it is left unreported and the price table decides.
//
// shortcut: reads the whole file, unlike claude's growing tail window; add a
// window if pi transcripts grow large enough to slow prompt operations.
func ExtractPiTailUsage(path string) ([]TailUsage, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var out []TailUsage
	for _, line := range bytes.Split(data, []byte("\n")) {
		if !bytes.Contains(line, []byte(`"usage"`)) {
			continue
		}
		var e piUsageEntry
		if err := json.Unmarshal(line, &e); err != nil {
			continue
		}
		u := e.Message.Usage
		id := strings.TrimSpace(e.ID)
		if e.Type != "message" || !strings.EqualFold(e.Message.Role, "assistant") || u == nil || id == "" ||
			u.Input+u.Output+u.CacheRead+u.CacheWrite+u.ReasoningTokens == 0 {
			continue
		}
		out = append(out, TailUsage{
			EntryUUID:           id,
			MessageID:           id,
			Model:               strings.TrimSpace(e.Message.Model),
			InputTokens:         u.Input,
			OutputTokens:        u.Output,
			ReasoningTokens:     u.ReasoningTokens,
			CacheReadTokens:     u.CacheRead,
			CacheCreationTokens: u.CacheWrite,
			Timestamp:           firstPiTimestamp(e.Message.Timestamp, parsePiTimestamp(e.Timestamp)),
			ReportedCostUSD:     u.Cost.Total,
			HasReportedCost:     u.Cost.Total > 0,
		})
	}
	return out, nil
}

// ExtractPiTailUsageFromSearchPaths reads pi usage only after verifying path
// resolves under one of the merged pi session roots (the default roots plus
// searchPaths).
func ExtractPiTailUsageFromSearchPaths(searchPaths []string, path string) ([]TailUsage, error) {
	safePath, err := validateSearchPathFile(mergePiSearchPaths(searchPaths), path)
	if err != nil {
		return nil, err
	}
	return ExtractPiTailUsage(safePath)
}
