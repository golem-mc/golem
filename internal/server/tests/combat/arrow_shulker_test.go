package combat

import (
	"math"
	"testing"

	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/internal/server/servertest"
	"github.com/golem-mc/golem/internal/server/world"
)

// vanilla: AbstractArrow.stepMoveAndHit, ProjectileUtil.getManyEntityHitResult
func TestAnArrowReflectsAtVanillasDistanceFromAClosedShulker(t *testing.T) {
	t.Parallel()
	srv, c := servertest.Join(t, servertest.HostileTestConfig(), "Watcher", servertest.OpsListing(t, "Watcher"))
	servertest.HoldTicks(t, srv)
	c.ChatCommand(`summon minecraft:shulker 3.5 64 3.5 {NoAI:1b}`)
	servertest.AwaitSystemChatSaying(t, c, "commands.summon.success")
	c.ChatCommand(`attribute @e[type=minecraft:shulker,limit=1] minecraft:scale base set 2`)
	servertest.AwaitSystemChatSaying(t, c, "commands.attribute.base_value.set.success")
	c.ChatCommand(`summon minecraft:arrow 4.1 65.5 7.5 {NoGravity:1b,Motion:[0d,0d,-0.5d]}`)
	servertest.AwaitSystemChatSaying(t, c, "commands.summon.success")
	arrows := srv.ArrowsInDimension(world.DimensionName)
	if len(arrows) != 1 {
		t.Fatalf("arrows = %v, want the summoned arrow", arrows)
	}
	arrowID := arrows[0].ID
	toldZ := 7.5
	var hitTold bool
	for tick := 1; tick <= 20; tick++ {
		var reversed bool
		c.ReadTicks(srv, 1, func(id int32, data []byte) bool {
			r := protocol.NewReader(data)
			if r.ReadVarInt() != arrowID {
				return false
			}
			switch id {
			case protocol.IDPlayEntityPosition, protocol.IDPlayEntityPositionRot:
				r.ReadVarInt() // movement properties
				r.ReadI16()
				r.ReadI16()
				toldZ += float64(r.ReadI16()) / 4096
			case protocol.IDPlaySetEntityMotion:
				_, _, vz := r.ReadLpVec3()
				reversed = vz > 0
			}
			return false
		})
		if tick == 6 {
			// The margin is 0.2 on this tick. The client must receive both
			// the hit location and reversed motion before the usual 20-tick update.
			hitTold = reversed && math.Abs(toldZ-(4.5+float64(float32(0.2)))) < 1.0/4096
		}
	}
	if !hitTold {
		t.Error("client was not told the reflected position and motion on the hit tick")
	}
	if math.Abs(toldZ-5.31787109375) > 1.0/4096 {
		t.Errorf("client's position at tick 20 = %v, want 5.31787109375", toldZ)
	}

	c.ChatCommand("tick freeze")
	servertest.AwaitSystemChatSaying(t, c, "commands.tick.status.frozen")
	if traffic := c.CountEntityMovement(srv, arrowID, 40); traffic.Positions < 2 {
		t.Errorf("frozen arrow received %d position corrections in 40 ticks, want at least two", traffic.Positions)
	}

	arrows = srv.ArrowsInDimension(world.DimensionName)
	if len(arrows) != 1 {
		t.Fatalf("arrows = %v, want the reflected arrow", arrows)
	}
	a := arrows[0]
	// Captured from the real vanilla client after the same shot, translated
	// from (150.5,100,155.5); movement packets quantize to 1/4096 block.
	if math.Abs(a.X-4.1) > 1.0/4096 || math.Abs(a.Y-65.5) > 1.0/4096 || math.Abs(a.Z-5.31787109375) > 1.0/4096 {
		t.Errorf("reflected position = (%v,%v,%v), want (4.1,65.5,5.31787109375)", a.X, a.Y, a.Z)
	}
	if math.Abs(a.VX) > 1.0/8000 || math.Abs(a.VY) > 1.0/8000 || math.Abs(a.VZ-0.040895354758820245) > 1.0/8000 {
		t.Errorf("reflected motion = (%v,%v,%v), want (0,0,0.040895354758820245)", a.VX, a.VY, a.VZ)
	}
}
