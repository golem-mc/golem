package items

import (
	"testing"

	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/internal/server/servertest"
)

// TestASummonedOrbKeepsItsTagsCount summons a merged orb worth five orbs of
// three on the player: picking all of it up gives fifteen points.
// vanilla: ExperienceOrb.readAdditionalSaveData, ExperienceOrb.playerTouch
func TestASummonedOrbKeepsItsTagsCount(t *testing.T) {
	t.Parallel()
	cfg, setup := servertest.ExperienceSetup(t, "Caller")
	srv, c, _ := servertest.JoinAs(t, cfg, "Caller", setup)

	c.ChatCommand("summon minecraft:experience_orb ~ ~ ~ {Value:3s,Count:5}")
	total := int32(-1)
	c.ReadTicksSkipping(srv, 100, nil, func(id int32, data []byte) bool {
		if id != protocol.IDPlaySetExperience {
			return false
		}
		r := protocol.NewReader(data)
		r.ReadF32()
		r.ReadVarInt()
		total = r.ReadVarInt()
		return total >= 15
	})
	if total != 15 {
		t.Errorf("total experience after taking the orb = %d, want 15 (Count 5 of Value 3)", total)
	}
}
