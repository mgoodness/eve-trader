package npcfactions_test

import (
	"testing"

	"github.com/mgoodness/eve-trader/internal/npcfactions"
)

func TestFaction(t *testing.T) {
	tests := []struct {
		name   string
		corpID int
		want   int
		ok     bool
	}{
		// Rens VI - Moon 8's owner, Brutor Tribe, is Minmatar Republic.
		{"Rens station owner", 1000049, 500002, true},
		{"Caldari Navy is Caldari State", 1000035, 500001, true},
		{"militia corp resolves too", 1000180, 500001, true},
		// Doomheim is one of the internal NPC corps the SDE gives no faction.
		{"no-faction internal corp", 1000001, 0, false},
		// A player corporation id is absent from the NPC table.
		{"unknown corp", 987654321, 0, false},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got, ok := npcfactions.Faction(tc.corpID)
			if ok != tc.ok || got != tc.want {
				t.Errorf("Faction(%d) = (%d, %v), want (%d, %v)", tc.corpID, got, ok, tc.want, tc.ok)
			}
		})
	}
}
