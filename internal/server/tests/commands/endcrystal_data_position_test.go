package commands

import (
	"testing"

	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/internal/server/servertest"
)

// vanilla: EntityDataAccessor.setData, Entity.load, ServerEntity.sendChanges
func TestCommandDataMovesAnEndCrystal(t *testing.T) {
	t.Parallel()
	for _, mode := range []string{"running", "frozen"} {
		t.Run(mode, func(t *testing.T) {
			t.Parallel()
			srv, c := servertest.Join(t, servertest.MobTestConfig(), "Writer", servertest.OpsListing(t, "Writer"))
			observer := servertest.DialServer(t, srv).Join("Observer")
			c.Settle()
			servertest.HoldTicks(t, srv)
			if mode == "frozen" {
				c.ChatCommand("tick freeze")
				c.ExpectTickingState(20, true)
				c.ExpectCommandFeedback("commands.tick.status.frozen")
			}
			c.ChatCommand("summon minecraft:end_crystal 1.75 64 0.5 {ShowBottom:0b}")
			crystal := observer.AwaitSpawn(protocol.EntityTypeEndCrystal)
			c.AwaitSpawn(protocol.EntityTypeEndCrystal)
			c.AwaitMetadataFor(crystal.EntityID)
			name := servertest.EntityDisplayComponent("minecraft:end_crystal", "entity.minecraft.end_crystal", crystal.Uuid)
			c.ExpectCommandFeedbackText("commands.summon.success", name)
			observer.ReadTicks(srv, 1, func(int32, []byte) bool { return false })

			c.ChatCommand("data merge entity @e[type=end_crystal,limit=1] {Pos:[9.75d,70d,2.5d]}")
			c.ExpectCommandFeedbackText("commands.data.entity.modified", name)
			c.ChatCommand("data get entity @e[type=end_crystal,limit=1] Pos")
			c.ExpectCommandFeedbackText("commands.data.entity.query", name, servertest.NBTArg("[9.75d, 70.0d, 2.5d]"))

			r := protocol.NewReader(observer.ExpectWithinTicks(srv, 1, protocol.IDPlayTeleportEntity))
			if id := r.ReadVarInt(); id != crystal.EntityID {
				t.Fatalf("moved entity %d, want crystal %d", id, crystal.EntityID)
			}
			r.ReadVarInt() // position path
			if x, y, z := r.ReadF64(), r.ReadF64(), r.ReadF64(); x != 9.75 || y != 70 || z != 2.5 {
				t.Errorf("observer received position (%v, %v, %v), want (9.75, 70, 2.5)", x, y, z)
			}
			if err := r.Err(); err != nil {
				t.Fatal(err)
			}
			observer.ExpectNoneWithinTicks(srv, 2, servertest.PacketAbout(crystal.EntityID,
				protocol.IDPlayTeleportEntity, protocol.IDPlayEntityPosition, protocol.IDPlayEntityPositionRot))
			c.ChatCommand("data get entity @e[type=end_crystal,limit=1] ShowBottom")
			c.ExpectCommandFeedbackText("commands.data.entity.query", name, servertest.NBTArg("0b"))
		})
	}
}
