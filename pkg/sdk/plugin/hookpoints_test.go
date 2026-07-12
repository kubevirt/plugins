package plugin

import (
	"sort"
	"testing"
)

func TestAllHookPointsReturnsCopy(t *testing.T) {
	first := AllHookPoints()
	sort.Strings(first)

	first[0] = "MUTATED"

	second := AllHookPoints()
	sort.Strings(second)

	for _, hookPoint := range second {
		if hookPoint == "MUTATED" {
			t.Fatal("AllHookPoints() returned the same backing array instead of a copy")
		}
	}
}
