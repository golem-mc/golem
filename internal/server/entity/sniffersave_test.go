package entity

import (
	"testing"

	"github.com/golem-mc/golem/nbt"
)

func TestASnifferKeepsItsCooldownAndExploredPositionsAcrossAReload(t *testing.T) {
	t.Parallel()
	m := NewMob(TypeSniffer, 1, 0, 64, 0, 1)
	m.sniffer.sniffCooldown = 37
	m.sniffer.explored = []snifferSpot{{"minecraft:the_nether", Waypoint{1, 2, 3}}, {"minecraft:overworld", Waypoint{-4, 65, 9}}}
	saved := nbt.Compound{}
	m.SaveTo(saved, nil)

	back := NewMob(TypeSniffer, 2, 0, 64, 0, 2)
	back.LoadFrom(saved, nil, nil)
	if back.sniffer.sniffCooldown != 37 {
		t.Errorf("cooldown = %d, want 37", back.sniffer.sniffCooldown)
	}
	if len(back.sniffer.explored) != 2 || back.sniffer.explored[0] != m.sniffer.explored[0] || back.sniffer.explored[1] != m.sniffer.explored[1] {
		t.Errorf("explored = %v, want %v", back.sniffer.explored, m.sniffer.explored)
	}
}
