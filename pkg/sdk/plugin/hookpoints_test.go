package plugin

import (
	"slices"
	"testing"
)

func TestAllNodeHookPointsReturnsCopy(t *testing.T) {
	first := AllNodeHookPoints()

	first[0] = "MUTATED"

	for _, hookPoint := range AllNodeHookPoints() {
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
	if !slices.Equal(expected, actual) {
		t.Fatalf("expected node hook points %v, got %v", expected, actual)
	}
}
