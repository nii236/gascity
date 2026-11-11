package sling

import (
	"testing"

	"github.com/gastownhall/gascity/internal/beadmeta"
	"github.com/gastownhall/gascity/internal/beads"
	"github.com/gastownhall/gascity/internal/config"
)

// TestSlingCookedRootRoutesItsUnroutedSteps is #9039: `gc formula cook` mints
// an expanded graph.v2 root whose default-bound steps carry no route, and the
// expanded root is not demand (#7497). Slinging that root must route those
// steps to the target, as `gc sling --formula` does at creation, or an
// on-demand agent never wakes. Steps the formula routed elsewhere, topology
// latches and closed steps keep what they have.
func TestSlingCookedRootRoutesItsUnroutedSteps(t *testing.T) {
	cfg := &config.City{Workspace: config.Workspace{Name: "test-city"}}
	deps := testDeps(cfg, nil, newFakeRunner().run)
	store := deps.Store
	deps.Router = &storeStampingRouter{fallback: store}

	mk := func(b beads.Bead) beads.Bead {
		t.Helper()
		got, err := store.Create(b)
		if err != nil {
			t.Fatalf("Create(%s): %v", b.Title, err)
		}
		return got
	}
	root := mk(beads.Bead{Title: "pancakes", Type: "task", Metadata: map[string]string{
		beadmeta.KindMetadataKey:             beadmeta.KindWorkflow,
		beadmeta.FormulaContractMetadataKey:  beadmeta.FormulaContractGraphV2,
		beadmeta.WorkflowExpandedMetadataKey: "true",
		beadmeta.ScopeKindMetadataKey:        "formula-cook",
	}})
	step := func(title string, meta map[string]string) beads.Bead {
		meta[beadmeta.RootBeadIDMetadataKey] = root.ID
		return mk(beads.Bead{Title: title, Type: "task", Metadata: meta})
	}
	plain := step("mix", map[string]string{})
	elsewhere := step("serve", map[string]string{beadmeta.RoutedToMetadataKey: "waiter"})
	scope := step("body", map[string]string{beadmeta.KindMetadataKey: beadmeta.KindScope})
	control := step("check", map[string]string{
		beadmeta.KindMetadataKey:     beadmeta.KindCheck,
		beadmeta.RoutedToMetadataKey: "control-dispatcher",
	})
	closed := step("done", map[string]string{})
	if err := store.Close(closed.ID); err != nil {
		t.Fatalf("Close: %v", err)
	}

	a := config.Agent{Name: "worker", MaxActiveSessions: intPtr(1)}
	if _, err := DoSling(SlingOpts{Target: a, BeadOrFormula: root.ID, NoConvoy: true}, deps, nil); err != nil {
		t.Fatalf("DoSling cooked root: %v", err)
	}

	meta := func(id, key string) string {
		t.Helper()
		b, err := store.Get(id)
		if err != nil {
			t.Fatalf("Get(%s): %v", id, err)
		}
		return b.Metadata[key]
	}
	for _, c := range []struct {
		name, id, key, want string
	}{
		{"unrouted step", plain.ID, beadmeta.RoutedToMetadataKey, "worker"},
		{"step routed elsewhere", elsewhere.ID, beadmeta.RoutedToMetadataKey, "waiter"},
		{"scope latch", scope.ID, beadmeta.RoutedToMetadataKey, ""},
		{"control step route", control.ID, beadmeta.RoutedToMetadataKey, "control-dispatcher"},
		{"control step execution route", control.ID, beadmeta.ExecutionRoutedToMetadataKey, "worker"},
		{"closed step", closed.ID, beadmeta.RoutedToMetadataKey, ""},
	} {
		if got := meta(c.id, c.key); got != c.want {
			t.Errorf("%s %s = %q, want %q", c.name, c.key, got, c.want)
		}
	}
}
