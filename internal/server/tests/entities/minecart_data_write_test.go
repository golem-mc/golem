package entities

import (
	"encoding/hex"
	"regexp"
	"slices"
	"testing"

	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/internal/server/servertest"
	"github.com/golem-mc/golem/registry"
)

// metadataIndices reads the field indices a metadata payload kept by a
// MetadataRecorder without a rewrite carries, in order.
func metadataIndices(t *testing.T, payload string) []uint8 {
	t.Helper()
	raw, err := hex.DecodeString(payload)
	if err != nil {
		t.Fatalf("payload %q: %v", payload, err)
	}
	rd := protocol.NewReader(raw)
	var indices []uint8
	for rd.Err() == nil {
		index := rd.ReadU8()
		if index == protocol.MetadataEnd {
			break
		}
		indices = append(indices, index)
		servertest.StepOverMetadataValue(rd, rd.ReadVarInt())
	}
	if err := rd.Err(); err != nil {
		t.Fatalf("payload %q: %v", payload, err)
	}
	return indices
}

// TestAMinecartsDataWriteTellsOnlyWhatItTurned merges a tag onto a held-up
// cart, first at rest and then rocking from a blow: neither write tells the
// no-gravity flag it left as it was, and the rocking goes on as the tick's
// winding down alone, its direction never told again.
// vanilla: EntityDataAccessor.setData, SynchedEntityData.set, packDirty
func TestAMinecartsDataWriteTellsOnlyWhatItTurned(t *testing.T) {
	t.Parallel()
	srv, connect := servertest.Start(t, servertest.MobTestConfig(), servertest.OpsListing(t, "Carter"))
	c := connect().Join("Carter")
	c.ChatCommand("gamerule spawn_mobs false")
	c.Settle()
	servertest.HoldTicks(t, srv)
	r := servertest.NewMetadataRecorder(t, srv, c)

	typ, ok := registry.EntityTypeID("minecraft:minecart")
	if !ok {
		t.Fatal("no entity type minecraft:minecart")
	}
	packets := r.Run(`summon minecraft:minecart 3.5 64 4.5 {NoGravity:1b,Tags:["held"]}`)
	id := servertest.SpawnedID(packets, typ)
	if id == 0 {
		t.Fatal("summoning the cart sent no spawn")
	}
	r.Read(5)

	r.Record("rest", id, r.Run(`data merge entity @e[tag=held,limit=1] {Tags:["held","rest"]}`))
	r.Record("rest", id, r.Read(5))
	if got := r.Got["rest"]; len(got) != 0 {
		t.Errorf("the write on the cart at rest was told %v, want nothing", got)
	}

	if !srv.HurtMinecart(id, 1, false) {
		t.Fatal("the blow on the cart did not land")
	}
	r.Read(1)
	r.Record("rocking", id, r.Run(`data merge entity @e[tag=held,limit=1] {Tags:["held"]}`))
	r.Record("rocking", id, r.Read(5))
	if len(r.Got["rocking"]) == 0 {
		t.Fatal("the rocking cart was told nothing after the write")
	}
	for _, payload := range r.Got["rocking"] {
		indices := metadataIndices(t, payload)
		if slices.Contains(indices, protocol.MetadataIndexNoGravity) {
			t.Errorf("the rocking cart was told its no-gravity flag again (%s)", payload)
		}
		if slices.Contains(indices, protocol.MetadataIndexVehicleShakeDirection) {
			t.Errorf("the rocking cart was told its rocking direction again (%s)", payload)
		}
	}
}

// TestAMinecartOnARailSavesNoVerticalMotion summons a cart onto a rail and
// lets it rest there: the rail zeroes its vertical motion every tick, so the
// saved Motion holds no gravity built up over the wait.
// vanilla: AbstractMinecart.applyNaturalSlowdown
func TestAMinecartOnARailSavesNoVerticalMotion(t *testing.T) {
	t.Parallel()
	srv, connect := servertest.Start(t, servertest.MobTestConfig(), servertest.OpsListing(t, "Carter"))
	c := connect().Join("Carter")
	c.ChatCommand("gamerule spawn_mobs false")
	c.Settle()
	servertest.HoldTicks(t, srv)
	srv.SetBlock(3, 64, 4, registry.BlockRail.DefaultState())
	c.ChatCommand(`summon minecraft:minecart 3.5 64 4.5 {Tags:["railed"]}`)
	c.Settle()
	servertest.RunTicks(t, srv, 40)

	compound := awaitEntityDataCompound(t, srv, c, "@e[tag=railed,limit=1]")
	m := regexp.MustCompile(`Motion: \[[^,\]]+, ([^,\]]+),`).FindStringSubmatch(compound)
	if m == nil {
		t.Fatalf("the cart's data carries no Motion: %s", compound)
	}
	if m[1] != "0.0d" {
		t.Errorf("the cart resting on the rail saved Motion[1] %s after 40 ticks, want 0.0d", m[1])
	}
}
