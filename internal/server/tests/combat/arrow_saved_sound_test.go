package combat

import (
	"fmt"
	"testing"

	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/internal/server/servertest"
	"github.com/golem-mc/golem/registry"
)

// TestLoadedArrowSoundEvent checks saved sounds survive a data write and reach
// impact packets, then reset after a block landing.
// vanilla: AbstractArrow.readAdditionalSaveData, onHitBlock, onHitEntity
func TestLoadedArrowSoundEvent(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name, kind, tag, want string
		mob                   bool
	}{
		{"crossbow_block", "arrow", `SoundEvent:"item.crossbow.hit"`, "minecraft:item.crossbow.hit", false},
		{"crossbow_mob", "arrow", `SoundEvent:"item.crossbow.hit"`, "minecraft:item.crossbow.hit", true},
		{"bell_block", "arrow", `SoundEvent:"block.bell.use"`, "minecraft:block.bell.use", false},
		{"bell_mob", "arrow", `SoundEvent:"minecraft:block.bell.use"`, "minecraft:block.bell.use", true},
		{"unknown", "arrow", `SoundEvent:"test:missing"`, "minecraft:entity.arrow.hit", false},
		{"wrong_type", "arrow", `SoundEvent:3`, "minecraft:entity.arrow.hit", false},
		{"absent", "arrow", "", "minecraft:entity.arrow.hit", false},
		{"spectral", "spectral_arrow", `SoundEvent:"block.bell.use"`, "minecraft:block.bell.use", false},
		{"trident", "trident", `SoundEvent:"block.bell.use"`, "minecraft:block.bell.use", false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			srv, c := servertest.Join(t, servertest.CombatTestConfig(), "Watcher", servertest.OpsListing(t, "Watcher"))
			servertest.HoldTicks(t, srv)
			c.MoveToGround(0.5, 64, 6.5, true)
			if tc.mob {
				c.ChatCommand(`summon cow 4.5 64 0.5 {NoAI:1b}`)
				cow := c.ExpectMobSpawn(protocol.EntityTypeCow)
				c.ExpectCommandFeedbackText("commands.summon.success",
					servertest.EntityDisplayComponent("minecraft:cow", "entity.minecraft.cow", cow.Uuid))
			} else {
				c.ChatCommand("setblock 4 65 0 stone")
				c.ExpectCommandFeedback("commands.setblock.success", "4", "65", "0")
			}
			tag := `NoGravity:1b,Tags:["sound"]`
			if tc.tag != "" {
				tag += "," + tc.tag
			}
			c.ChatCommand(fmt.Sprintf("summon %s 1.5 65 0.5 {%s}", tc.kind, tag))
			typeID, _ := registry.EntityTypeID("minecraft:" + tc.kind)
			shaft := c.AwaitSpawn(typeID)
			name := servertest.EntityDisplayComponent("minecraft:"+tc.kind, "entity.minecraft."+tc.kind, shaft.Uuid)
			c.ExpectCommandFeedbackText("commands.summon.success", name)
			query := func(want string) {
				t.Helper()
				c.ChatCommand("data get entity @e[tag=sound,limit=1] SoundEvent")
				c.ExpectCommandFeedbackText("commands.data.entity.query", name, servertest.NBTArg(fmt.Sprintf("%q", want)))
			}
			query(tc.want)
			c.ChatCommand(`data merge entity @e[tag=sound,limit=1] {Motion:[1.0d,0.0d,0.0d]}`)
			c.ExpectCommandFeedbackText("commands.data.entity.modified", name)
			wantID, _ := registry.SoundEventID(tc.want)
			if !c.ReadTicks(srv, 6, func(id int32, data []byte) bool {
				return id == protocol.IDPlaySoundEffect && protocol.NewReader(data).ReadVarInt() == wantID+1
			}) {
				t.Fatalf("impact did not play %s", tc.want)
			}
			if !tc.mob {
				query("minecraft:entity.arrow.hit")
			}
		})
	}
}
