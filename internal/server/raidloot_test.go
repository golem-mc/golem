package server

import (
	"testing"

	"github.com/golem-mc/golem/internal/server/entity"
	"github.com/golem-mc/golem/internal/server/raid"
)

func TestMobLootRaidUsesMembershipAndBlockDistance(t *testing.T) {
	centers := []raid.Pos{{X: 3, Y: 64, Z: 3}}
	for _, tc := range []struct {
		name         string
		kind         entity.MobType
		x, y, z      float64
		member, want bool
	}{
		{"nearby unenrolled captain", entity.TypePillager, 3.5, 64, 3.5, false, true},
		{"fraction floors inside radius", entity.TypePillager, 98.99, 64, 3.5, false, true},
		{"exact radius excluded", entity.TypePillager, 99, 64, 3.5, false, false},
		{"vertical radius excluded", entity.TypePillager, 3.5, 160, 3.5, false, false},
		{"negative block floors outside", entity.TypePillager, -92.01, 64, 3.5, false, false},
		{"member outside radius", entity.TypePillager, 200, 64, 3.5, true, true},
		{"nonraider near raid", entity.TypeCow, 3.5, 64, 3.5, false, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			ent := &mobEntity{mob: &entity.Mob{Type: tc.kind, X: tc.x, Y: tc.y, Z: tc.z}}
			if tc.member {
				ent.raider = &raiderRef{raid: &raid.Raid{}}
			}
			if got := mobHasLootRaid(ent, centers); got != tc.want {
				t.Errorf("has raid = %v, want %v", got, tc.want)
			}
		})
	}
}
