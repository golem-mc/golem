package entities

import (
	"encoding/hex"
	"testing"

	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/internal/server/command"
	"github.com/golem-mc/golem/internal/server/entity"
	"github.com/golem-mc/golem/internal/server/player"
	"github.com/golem-mc/golem/internal/server/servertest"
	"github.com/golem-mc/golem/internal/server/world"
)

// hookedField reads the hooked-entity field out of a metadata payload kept
// by a MetadataRecorder without a rewrite: its value on the wire, and
// whether the payload carried it at all.
func hookedField(t *testing.T, payload string) (value int32, told bool) {
	t.Helper()
	raw, err := hex.DecodeString(payload)
	if err != nil {
		t.Fatalf("payload %q: %v", payload, err)
	}
	rd := protocol.NewReader(raw)
	for rd.Err() == nil {
		index := rd.ReadU8()
		if index == protocol.MetadataEnd {
			break
		}
		typ := rd.ReadVarInt()
		if index == protocol.MetadataIndexHookedEntity && typ == protocol.MetadataVarInt {
			value, told = rd.ReadVarInt(), true
			continue
		}
		servertest.StepOverMetadataValue(rd, typ)
	}
	if err := rd.Err(); err != nil {
		t.Fatalf("payload %q: %v", payload, err)
	}
	return value, told
}

// TestAFreeHookTellsNoHookedBody checks the hooked-entity field of a hook
// caught in nothing: the let-go of a body sends 0, the "none" of the
// entity ID plus one, and a free hook's bite shown to a joiner leaves the
// field at its default and out of the packet.
// vanilla: FishingHook.setHookedEntity, SynchedEntityData.getNonDefaultValues
func TestAFreeHookTellsNoHookedBody(t *testing.T) {
	t.Parallel()
	srv, connect := servertest.Start(t, servertest.BowTestConfig(), servertest.OpsListing(t, "Angler"))
	c := connect().Join("Angler")
	srv.GiveSlot("Angler", 36, protocol.ItemStack{ItemID: servertest.MustItem("minecraft:fishing_rod"), Count: 1})
	c.ChatCommand("gamerule spawn_mobs false")
	c.ChatCommand("weather clear")
	c.Settle()
	servertest.HoldTicks(t, srv)
	r := servertest.NewMetadataRecorder(t, srv, c)

	cowID := srv.SpawnMob("minecraft:cow", 3.5, 64, 0.5)
	if !srv.SetMobNoAI(cowID) {
		t.Fatal("the cow never stood still")
	}
	c.MoveTo(0.5, 64, 0.5, -90, 10)
	r.Read(1)
	hookID := castHook(t, r, -90, 10)
	awaitBobberMetadata(t, r, "take", hookID, 40)
	if v, told := hookedField(t, r.Got["take"][0]); !told || v != cowID+1 {
		t.Fatalf("the take told hooked %d (told %v), want the cow's ID plus one, %d", v, told, cowID+1)
	}
	c.ChatCommand("kill @e[type=minecraft:cow]")
	awaitBobberMetadata(t, r, "let-go", hookID, 40)
	if v, told := hookedField(t, r.Got["let-go"][0]); !told || v != 0 {
		t.Errorf("the let-go told hooked %d (told %v), want 0 for none", v, told)
	}
	servertest.UseHeld(c, -90, 10)
	r.Read(2)

	for x := int32(1); x <= 3; x++ {
		for z := int32(2); z <= 4; z++ {
			srv.SetBlock(x, 63, z, servertest.MustState(t, "minecraft:water", nil))
		}
	}
	c.MoveTo(2.5, 64, 1.5, 0, 40)
	r.Read(1)
	hookID = castHook(t, r, 0, 40)
	r.Read(20)
	if !srv.WindBobberBite("Angler") {
		t.Fatal("no hook was out to wind")
	}
	awaitBobberMetadata(t, r, "bite", hookID, 40)
	latecomerMetadata(r, connect, "Gazer", 0, map[string]int32{"latecomer": hookID})
	if len(r.Got["latecomer"]) == 0 {
		t.Fatal("the joiner was shown no metadata of the biting hook")
	}
	for _, payload := range r.Got["latecomer"] {
		if v, told := hookedField(t, payload); told {
			t.Errorf("the joiner was told the free hook's hooked field %d, want it left at its default", v)
		}
	}
	c.Disconnect()
}

// castHook uses the rod and returns the ID of the hook the cast spawned.
func castHook(t *testing.T, r *servertest.MetadataRecorder, yaw, pitch float32) int32 {
	t.Helper()
	servertest.UseHeld(r.C, yaw, pitch)
	packets := r.Read(1)
	hookID := servertest.SpawnedID(packets, protocol.EntityTypeFishingBobber)
	if hookID == 0 {
		t.Fatal("the cast spawned no fishing bobber")
	}
	return hookID
}

// awaitBobberMetadata runs ticks one at a time, up to n, until the client is
// sent metadata for the hook, and keeps it under the case's name.
func awaitBobberMetadata(t *testing.T, r *servertest.MetadataRecorder, name string, hookID int32, n int) {
	t.Helper()
	for range n {
		before := len(r.Got[name])
		r.Record(name, hookID, r.Read(1))
		if len(r.Got[name]) > before {
			return
		}
	}
	t.Fatalf("%s: the hook was sent no metadata within %d ticks", name, n)
}

// latecomerMetadata joins a second player named player and keeps the
// metadata payloads its join and the ticks after it showed it for each body
// of cases under the case's name; the player leaves again after those
// ticks. The loop stays held: the join's own wait for ticks runs out rather
// than moving the world.
func latecomerMetadata(r *servertest.MetadataRecorder, connect func() *servertest.Client, player string, ticks int, cases map[string]int32) {
	r.T.Helper()
	late := connect()
	late.Login(player)
	late.Configure()
	var packets []servertest.PushedPacket
	keep := func(id int32, data []byte) {
		packets = append(packets, servertest.PushedPacket{Id: id, Data: append([]byte(nil), data...)})
	}
	late.ReadJoin(keep)
	late.MarkLoaded()
	late.ReadTicks(r.Srv, ticks, func(id int32, data []byte) bool {
		keep(id, data)
		return false
	})
	for name, id := range cases {
		r.Record(name, id, packets)
	}
	late.Disconnect()
}

// TestATeleportedHookIsToldOncePerMovementTick checks that twenty /tp of a
// cast hook in one tick reach the watcher as one Teleport Entity, sent by the
// hook's movement tick.
// vanilla: Entity.teleportTo, ServerEntity.sendChanges
func TestATeleportedHookIsToldOncePerMovementTick(t *testing.T) {
	t.Parallel()
	srv, connect := servertest.Start(t, servertest.BowTestConfig(), servertest.OpsListing(t, "Angler"))
	c := connect().Join("Angler")
	srv.GiveSlot("Angler", 36, protocol.ItemStack{ItemID: servertest.MustItem("minecraft:fishing_rod"), Count: 1})
	c.ChatCommand("gamerule spawn_mobs false")
	c.Settle()
	servertest.HoldTicks(t, srv)
	r := servertest.NewMetadataRecorder(t, srv, c)
	hookID := castHook(t, r, -90, 10)

	hooks := srv.SelectedEntities("minecraft:fishing_bobber")
	if len(hooks) != 1 {
		t.Fatalf("selected %d hooks, want 1", len(hooks))
	}
	at := hooks[0].Position()
	for j := range 20 {
		hooks[0].Teleport(command.Move{Pos: player.Position{X: at.X + float64(j+1), Y: at.Y + 5, Z: at.Z}},
			world.DimensionName, "test")
	}
	told := 0
	for _, p := range r.Read(entity.BobberInterval) {
		if p.Id == protocol.IDPlayTeleportEntity && protocol.NewReader(p.Data).ReadVarInt() == hookID {
			told++
		}
	}
	if told != 1 {
		t.Errorf("the hook was sent %d Teleport Entity packets for twenty teleports in one tick, want 1", told)
	}
	c.Disconnect()
}
