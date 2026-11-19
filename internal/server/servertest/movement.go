package servertest

import (
	"bytes"
	"testing"
	"time"

	"github.com/golem-mc/golem/internal/config"
	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/internal/server"
	"github.com/golem-mc/golem/internal/server/player"
	"github.com/golem-mc/golem/internal/server/world"
	"github.com/golem-mc/golem/registry"
)

// StartFallFlying sends the Player Command a client sends when it presses the
// gliding key; the server answers by starting the fall.
func StartFallFlying(c *Client) {
	c.T.Helper()
	c.Send(protocol.IDPlayPlayerCommand, func(b *protocol.Buffer) {
		b.PutVarInt(c.PlayerEntityID) // the ignored self-naming
		b.PutVarInt(protocol.PlayerCommandStartFlyingElytra)
		b.PutVarInt(0) // no horse jump
	})
}

// AwaitSystemChatSaying reads packets until a System Chat whose payload
// carries the translation key arrives, and puts back everything it stepped
// over. It matches the key rather than the whole payload because the line's
// arguments carry a fresh identity.
func AwaitSystemChatSaying(t *testing.T, c *Client, key string) {
	t.Helper()
	var skipped []PushedPacket
	defer func() { c.Unread(skipped...) }()
	for packetID, data := range c.Packets(10 * time.Second) {
		if packetID == protocol.IDPlaySystemChat && bytes.Contains(data, []byte(key)) {
			return
		}
		skipped = append(skipped, PushedPacket{Id: packetID, Data: data})
	}
	t.Fatalf("no system chat saying %s arrived", key)
}

// StartExecuteWorldForTwo is StartExecuteWorld with a second player standing
// beside the runner at the spawn. It returns the guest's entity ID.
func StartExecuteWorldForTwo(t *testing.T, runner, guest string) (*server.Server, *Client, *Client, int32) {
	t.Helper()
	cfg := config.Default()
	cfg.OnlineMode = false
	srv, connect := Start(t, cfg, OpsListing(t, runner))
	r := connect().Join(runner)
	g := connect().Join(guest)
	guestID := g.PlayerEntityID
	r.ExpectPlayerAdded(guest)
	r.Settle()
	return srv, r, g, guestID
}

// ChargeByHand runs the stopped loop's survival pass alone until the named
// player's swing has wound ticks, the mobs left where they were planted.
func ChargeByHand(t *testing.T, srv *server.Server, name string, ticks int32) {
	t.Helper()
	for i := int32(0); srv.AttackTicks(name) < ticks; i++ {
		if i == 2*ticks {
			t.Fatalf("%d hand ticks left %s's swing at %d", i, name, srv.AttackTicks(name))
		}
		srv.TickSurvival()
	}
}

// Stab sends the Player Action a client sends on jabbing with a spear.
func (c *Client) Stab() {
	c.T.Helper()
	c.Send(protocol.IDPlayPlayerAction, func(b *protocol.Buffer) {
		b.PutVarInt(protocol.PlayerActionStab)
		b.PutPosition(0, 0, 0)
		b.PutI8(0)     // face
		b.PutVarInt(0) // sequence
	})
}

// LadderScenario raises the two blocks of stone wall the ladder tests hang
// their ladders on, at (x, y, z) and one above it, and leaves both players'
// streams clean of the placements' own traffic: the watcher, when one is
// given, is told each stone, the actor the ack and the tail's echoes each
// placement ends with.
func LadderScenario(t *testing.T, srv *server.Server, actor, watcher *Client, entityID int32, x, y, z int32) {
	t.Helper()
	stone, _ := registry.ItemID("minecraft:stone")
	actor.SetCreativeSlot(player.HotbarSlot, protocol.ItemStack{ItemID: stone, Count: 1})
	if watcher != nil {
		watcher.ExpectEquipment(entityID)
	}

	actor.PlaceBlock(x, y, z, protocol.FaceTop)
	if watcher != nil {
		if got := watcher.ExpectBlockUpdate(); got.X != x || got.Y != y || got.Z != z || got.State != 1 {
			t.Fatalf("wall stone update = (%d,%d,%d) state %d, want (%d,%d,%d) stone",
				got.X, got.Y, got.Z, got.State, x, y, z)
		}
	}
	if seq := actor.ExpectBlockAck(); seq != 1 {
		t.Fatalf("wall stone ack sequence = %d, want 1", seq)
	}
	if echoes := actor.ExpectUseEchoes(x, y, z, protocol.FaceTop); echoes[0] != 1 || echoes[1] != 0 {
		t.Fatalf("wall stone echoes = %v, want the stone (%d) and the air (%d)", echoes, 1, 0)
	}

	actor.PlaceBlock(x, y+1, z, protocol.FaceTop)
	if watcher != nil {
		if got := watcher.ExpectBlockUpdate(); got.X != x || got.Y != y+1 || got.Z != z || got.State != 1 {
			t.Fatalf("wall stone update = (%d,%d,%d) state %d, want (%d,%d,%d) stone",
				got.X, got.Y, got.Z, got.State, x, y+1, z)
		}
	}
	actor.ExpectBlockAck()
	if echoes := actor.ExpectUseEchoes(x, y+1, z, protocol.FaceTop); echoes[0] != 1 || echoes[1] != 0 {
		t.Fatalf("wall stone echoes = %v, want the stone (%d) and the air (%d)", echoes, 1, 0)
	}

	if got := srv.BlockAt(x, y, z); got != 1 {
		t.Fatalf("wall stone = %d, want 1", got)
	}
}

// MetadataPose reads the pose a player's Set Entity Metadata carries, and
// whether it carried one.
func MetadataPose(t *testing.T, data []byte) (int32, bool) {
	t.Helper()
	r := protocol.NewReader(data)
	r.ReadVarInt() // entity ID
	pose, found := int32(0), false
	for r.Err() == nil {
		index := r.ReadU8()
		if index == protocol.MetadataEnd {
			break
		}
		fieldType := r.ReadVarInt()
		switch index {
		case protocol.MetadataIndexEntityFlags, protocol.MetadataIndexSkinParts,
			protocol.MetadataIndexHandState:
			r.ReadU8()
		case protocol.MetadataIndexAir:
			r.ReadVarInt()
		case protocol.MetadataIndexMainHand:
			r.ReadVarInt()
		case protocol.MetadataIndexPose:
			pose, found = r.ReadVarInt(), true
		default:
			t.Fatalf("unexpected metadata field %d of type %d", index, fieldType)
		}
	}
	if err := r.Err(); err != nil {
		t.Fatalf("entity metadata: %v", err)
	}
	return pose, found
}

// SoleItemY is the height of the one item standing in the overworld, failing
// the test when the world holds none or more than one.
func SoleItemY(t *testing.T, srv *server.Server) float64 {
	t.Helper()
	items := srv.ItemsInDimension(world.DimensionName)
	if len(items) != 1 {
		t.Fatalf("items standing in the overworld = %d, want the one under test", len(items))
	}
	return items[0].Y
}

// SoleTNTY is the height of the one primed TNT burning in the overworld,
// failing the test when the world holds none or more than one.
func SoleTNTY(t *testing.T, srv *server.Server) float64 {
	t.Helper()
	tnts := srv.TNTsInDimension(world.DimensionName)
	if len(tnts) != 1 {
		t.Fatalf("primed TNT burning in the overworld = %d, want the one under test", len(tnts))
	}
	return tnts[0].Y
}
