package plugin

import (
	"slices"
	"testing"
)

func TestAllNodeHookPointsReturnsCopy(t *testing.T) {
	first := AllNodeHookPoints()
	slices.Sort(first)

	first[0] = "MUTATED"

	second := AllNodeHookPoints()
	slices.Sort(second)

	for _, hookPoint := range second {
		if hookPoint == "MUTATED" {
			t.Fatal("AllNodeHookPoints() returned the same backing array instead of a copy")
		}
	}
}

func TestAllNodeHookPointsMatchesConstants(t *testing.T) {
	expected := []NodeHookPoint{
		NodeHookPreVMStart,
		NodeHookPostVMStart,
		NodeHookOnVMStop,
		NodeHookPostVMStop,
		NodeHookPreMigrationSource,
		NodeHookPreMigrationTarget,
		NodeHookPostMigrationTarget,
	}

	actual := AllNodeHookPoints()
	if len(actual) != len(expected) {
		t.Fatalf("expected %d node hook points, got %d", len(expected), len(actual))
	}

	for i, hookPoint := range expected {
		if actual[i] != hookPoint {
			t.Fatalf("node hook point %d: expected %q, got %q", i, hookPoint, actual[i])
		}
	}
}
