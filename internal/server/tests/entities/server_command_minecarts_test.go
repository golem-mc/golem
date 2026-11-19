package entities

import (
	"github.com/golem-mc/golem/internal/server/servertest"
	"testing"
	"time"

	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/internal/server/world"
	"github.com/google/uuid"
)

// TestEntitySelectorsReachMinecarts checks that the selectors name a plain
// minecart the server stands: /kill, /tag and /data get entity all reach it,
// and the resolver reads it directly.
func TestEntitySelectorsReachMinecarts(t *testing.T) {
	t.Parallel()
	srv, c := servertest.Join(t, servertest.MobTestConfig(), "Carter", servertest.OpsListing(t, "Carter"))

	// The loop is stopped so the cart stays where it was laid to be read,
	// tagged and killed rather than falling away first.
	srv.StopTick()

	cartID := srv.SpawnMinecart(1.5, 64, 0.5)
	if cartID == 0 {
		t.Fatal("SpawnMinecart came to nothing")
	}
	carts := srv.MinecartsInDimension(world.DimensionName)
	if len(carts) != 1 {
		t.Fatalf("minecarts in the overworld = %d, want 1", len(carts))
	}
	identity, err := uuid.Parse(carts[0].UUID)
	if err != nil {
		t.Fatalf("parse minecart identity: %v", err)
	}
	display := servertest.EntityDisplayComponent("minecraft:minecart", "entity.minecraft.minecart", identity)

	// The resolver alone reads the cart, the symptom the issue reported.
	if got := srv.SelectedEntities("minecraft:minecart"); len(got) != 1 {
		t.Errorf("the selector resolved %d minecarts, want 1", len(got))
	}

	// /tag reaches the cart, and the tag rides its saved compound.
	c.ChatCommand("tag @e[type=minecraft:minecart] add Horse")
	servertest.AwaitCommandFeedbackText(t, c, "commands.tag.add.success.single",
		protocol.Text{Text: "Horse"}, display)
	if got := srv.MinecartTags(cartID); len(got) != 1 || got[0] != "Horse" {
		t.Errorf("minecart tags = %v, want [Horse]", got)
	}

	// /data get entity reads the compound back through the same selector.
	c.ChatCommand("data get entity @e[type=minecraft:minecart,limit=1] Tags")
	servertest.AwaitCommandFeedbackText(t, c, "commands.data.entity.query",
		display, servertest.FeedbackArgsForTest("commands.data.entity.query", []string{`["Horse"]`})[0])

	// /kill discards the cart out of the world.
	c.ChatCommand("kill @e[type=minecraft:minecart]")
	servertest.AwaitCommandFeedbackText(t, c, "commands.kill.success.single", display)
	if _, _, _, _, ok := srv.MinecartState(cartID); ok {
		t.Error("the killed minecart is still in the world")
	}
	c.Disconnect()
}

// TestMinecartRotationIsWrittenYawFirst: the cart's compound writes the yaw
// into Rotation[0], the base tag's own order, so the selector's nbt match
// reads the yaw back and a /data merge that rebuilds the compound from the
// cart keeps a yaw an earlier merge set.
func TestMinecartRotationIsWrittenYawFirst(t *testing.T) {
	t.Parallel()
	srv, c := servertest.Join(t, servertest.MobTestConfig(), "Tuner", servertest.OpsListing(t, "Tuner"))

	// The loop is stopped so the cart stays under the writes.
	srv.StopTick()

	cartID := srv.SpawnMinecart(1.5, 64, 0.5)
	if cartID == 0 {
		t.Fatal("SpawnMinecart came to nothing")
	}
	carts := srv.MinecartsInDimension(world.DimensionName)
	if len(carts) != 1 {
		t.Fatalf("minecarts in the overworld = %d, want 1", len(carts))
	}
	identity, err := uuid.Parse(carts[0].UUID)
	if err != nil {
		t.Fatalf("parse minecart identity: %v", err)
	}
	display := servertest.EntityDisplayComponent("minecraft:minecart", "entity.minecraft.minecart", identity)

	// A write sets the yaw through Rotation[0].
	c.ChatCommand("data merge entity @e[type=minecraft:minecart,limit=1] {Rotation:[90f,0f]}")
	servertest.AwaitCommandFeedbackText(t, c, "commands.data.entity.modified", display)
	if _, _, _, yaw, ok := srv.MinecartState(cartID); !ok || yaw != 90 {
		t.Fatalf("yaw after the merge = %v (%v), want 90", yaw, ok)
	}
	// A second write that rebuilds the compound from the cart keeps the
	// yaw, where writing it into the pitch slot would reset it to 0.
	c.ChatCommand(`data merge entity @e[type=minecraft:minecart,limit=1] {Tags:["M"]}`)
	servertest.AwaitCommandFeedbackText(t, c, "commands.data.entity.modified", display)
	if _, _, _, yaw, ok := srv.MinecartState(cartID); !ok || yaw != 90 {
		t.Errorf("yaw after the second merge = %v (%v), want 90", yaw, ok)
	}
	// The selector's own nbt read finds the yaw in the first slot.
	c.ChatCommand("tag @e[type=minecraft:minecart,nbt={Rotation:[90f,0f]}] add Spin")
	servertest.AwaitCommandFeedbackText(t, c, "commands.tag.add.success.single",
		protocol.Text{Text: "Spin"}, display)
	c.Disconnect()
}

// TestMinecartCarriesFlippedAndTickedFlags: the cart's compound always
// carries FlippedRotation and HasTicked, vanilla's own two save fields, so a
// selector's nbt read finds both and HasTicked flips once the cart ticks.
// vanilla: AbstractMinecart.addAdditionalSaveData
func TestMinecartCarriesFlippedAndTickedFlags(t *testing.T) {
	t.Parallel()
	srv, c := servertest.Join(t, servertest.MobTestConfig(), "Marker", servertest.OpsListing(t, "Marker"))

	// The loop is stopped so the cart has not ticked until the test says so.
	srv.StopTick()

	cartID := srv.SpawnMinecart(1.5, 64, 0.5)
	if cartID == 0 {
		t.Fatal("SpawnMinecart came to nothing")
	}
	carts := srv.MinecartsInDimension(world.DimensionName)
	if len(carts) != 1 {
		t.Fatalf("minecarts in the overworld = %d, want 1", len(carts))
	}
	identity, err := uuid.Parse(carts[0].UUID)
	if err != nil {
		t.Fatalf("parse minecart identity: %v", err)
	}
	display := servertest.EntityDisplayComponent("minecraft:minecart", "entity.minecraft.minecart", identity)

	// A cart that has not ticked carries HasTicked, and it is not flipped.
	c.ChatCommand("tag @e[type=minecraft:minecart,nbt={HasTicked:1b}] add Fresh")
	servertest.AwaitCommandFeedbackText(t, c, "commands.tag.add.success.single",
		protocol.Text{Text: "Fresh"}, display)
	c.ChatCommand("tag @e[type=minecraft:minecart,nbt={FlippedRotation:0b}] add Upright")
	servertest.AwaitCommandFeedbackText(t, c, "commands.tag.add.success.single",
		protocol.Text{Text: "Upright"}, display)

	// Once it ticks, the flag flips and the first selector no longer names
	// it.
	srv.TickMinecarts(1)
	c.ChatCommand("tag @e[type=minecraft:minecart,nbt={HasTicked:0b}] add Ticked")
	servertest.AwaitCommandFeedbackText(t, c, "commands.tag.add.success.single",
		protocol.Text{Text: "Ticked"}, display)
	c.ChatCommand(`tag @e[type=minecraft:minecart,nbt={HasTicked:1b}] add Fresh`)
	c.ExpectTagRefusal("argument.entity.notfound.entity")
	c.Disconnect()
}

// TestMinecartNoGravityReachesTheClient: NoGravity is a writable synched
// flag, so a /data merge that turns it tells the clients shown the cart,
// where sending no metadata for the write would leave them with the old
// state.
func TestMinecartNoGravityReachesTheClient(t *testing.T) {
	t.Parallel()
	srv, c := servertest.Join(t, servertest.MobTestConfig(), "Holder", servertest.OpsListing(t, "Holder"))

	cartID := srv.SpawnMinecart(1.5, 64, 0.5)
	if cartID == 0 {
		t.Fatal("SpawnMinecart came to nothing")
	}
	spawn := c.AwaitSpawn(servertest.CartMinecartType(t))
	if spawn.EntityID != cartID {
		t.Fatalf("the spawned cart = %d, want the server's %d", spawn.EntityID, cartID)
	}

	// The write turns the flag; the client is shown it in a metadata update
	// rather than left holding the state the spawn introduced.
	c.ChatCommand("data merge entity @e[type=minecraft:minecart,limit=1] {NoGravity:1b}")
	entries := c.AwaitMetadataFor(cartID)
	if m := servertest.MetaOf(entries, protocol.MetadataIndexNoGravity); m == nil || !m.FlagVal {
		t.Errorf("the write's metadata carried no-gravity = %+v, want the flag set", m)
	}
	c.Disconnect()
}

// TestMinecartClearingNoGravityReachesTheClient: a /data merge that clears
// the flag is news like one that sets it, so the client's copy of the cart
// comes back to the default rather than being left holding the flag.
func TestMinecartClearingNoGravityReachesTheClient(t *testing.T) {
	t.Parallel()
	srv, c := servertest.Join(t, servertest.MobTestConfig(), "Holder", servertest.OpsListing(t, "Holder"))

	cartID := srv.SpawnMinecart(1.5, 64, 0.5)
	if cartID == 0 {
		t.Fatal("SpawnMinecart came to nothing")
	}
	if spawn := c.AwaitSpawn(servertest.CartMinecartType(t)); spawn.EntityID != cartID {
		t.Fatalf("the spawned cart = %d, want the server's %d", spawn.EntityID, cartID)
	}

	c.ChatCommand("data merge entity @e[type=minecraft:minecart,limit=1] {NoGravity:1b}")
	if m := servertest.MetaOf(c.AwaitMetadataFor(cartID), protocol.MetadataIndexNoGravity); m == nil || !m.FlagVal {
		t.Fatalf("setting no-gravity did not reach the client: %+v", m)
	}

	// The flag turned off leaves the cart at its fresh defaults, which the
	// spawn payload would send nothing for; the write must still tell it.
	c.ChatCommand("data merge entity @e[type=minecraft:minecart,limit=1] {NoGravity:0b}")
	if m := servertest.MetaOf(c.AwaitMetadataFor(cartID), protocol.MetadataIndexNoGravity); m == nil || m.FlagVal {
		t.Errorf("clearing no-gravity = %+v, want the flag cleared", m)
	}
	c.Disconnect()
}

// TestTeleportMovesMinecartsBySelector checks that /tp reaches a cart a
// selector names, in this dimension and across to the nether by twin of the
// same identity, with none left behind.
func TestTeleportMovesMinecartsBySelector(t *testing.T) {
	t.Parallel()
	srv, c := servertest.Join(t, servertest.MobTestConfig(), "Mover", servertest.OpsListing(t, "Mover"))

	// The loop is stopped so the cart stays under the command.
	srv.StopTick()

	cartID := srv.SpawnMinecart(1.5, 64, 0.5)
	if cartID == 0 {
		t.Fatal("SpawnMinecart came to nothing")
	}
	before := srv.MinecartsInDimension(world.DimensionName)
	if len(before) != 1 {
		t.Fatalf("minecarts in the overworld = %d, want 1", len(before))
	}

	// Same dimension: the cart gets its own Teleport Entity to the place
	// the command named.
	c.ChatCommand("tp @e[type=minecraft:minecart] 3.5 64 7.5")
	// The tick tells the cart: on its movement schedule, within one period.
	servertest.PollUntil(t, 10*time.Second, func() bool {
		got := srv.SelectedEntities("minecraft:minecart")
		return len(got) == 1 && got[0].Position().X == 3.5
	})
	for range 3 { // the cart's movement period
		srv.RunTick()
	}
	data, ok := servertest.ReadUntil(t, c, servertest.ExpectTimeout, protocol.IDPlayTeleportEntity)
	if !ok {
		t.Fatal("no teleport entity arrived for the minecart")
	}
	r := protocol.NewReader(data)
	if id := r.ReadVarInt(); id != cartID {
		t.Fatalf("teleported entity = %d, want the minecart's %d", id, cartID)
	}
	r.ReadVarInt() // position path
	if x, y, z := r.ReadF64(), r.ReadF64(), r.ReadF64(); x != 3.5 || y != 64 || z != 7.5 {
		t.Errorf("the minecart stands at (%v, %v, %v), want (3.5, 64, 7.5)", x, y, z)
	}

	// Across dimensions: the cart is picked in the overworld and the tail
	// carries the @s it named into the nether, by twin of the same
	// identity, with none left behind.
	c.ChatCommand("execute as @e[type=minecraft:minecart] in minecraft:the_nether run tp @s 0.5 64 0.5")
	servertest.PollUntil(t, 10*time.Second, func() bool {
		return len(srv.MinecartsInDimension(world.NetherName)) == 1
	})
	nether := srv.MinecartsInDimension(world.NetherName)
	if len(nether) != 1 {
		t.Fatalf("minecarts in the nether = %d, want 1", len(nether))
	}
	if nether[0].UUID != before[0].UUID {
		t.Errorf("the crossed cart's identity = %v, want the cart's %v", nether[0].UUID, before[0].UUID)
	}
	if got := srv.MinecartsInDimension(world.DimensionName); len(got) != 0 {
		t.Errorf("minecarts left in the overworld = %d, want none", len(got))
	}
	c.Disconnect()
}

// TestATeleportedCartCarriesItsRiderAcross: a cart named by /tp's selector
// is carried over as a twin, and Entity.teleportCrossDimension carries the
// riders over with it, remounting them on the far side and keeping the
// flags the twin would otherwise drop.
// vanilla: Entity.teleportCrossDimension
func TestATeleportedCartCarriesItsRiderAcross(t *testing.T) {
	t.Parallel()
	cfg := servertest.BlockTestConfig()
	srv, c := servertest.Join(t, cfg, "Ferryman", servertest.OpsListing(t, "Ferryman"))

	servertest.CartRails(t, srv, 3, 6)
	cartID := srv.SpawnMinecart(3.5, 64.0625, 1.5)
	if cartID == 0 {
		t.Fatal("SpawnMinecart came to nothing")
	}
	before := srv.MinecartsInDimension(world.DimensionName)
	if len(before) != 1 {
		t.Fatalf("minecarts in the overworld = %d, want 1", len(before))
	}
	identity, err := uuid.Parse(before[0].UUID)
	if err != nil {
		t.Fatalf("parse minecart identity: %v", err)
	}
	display := servertest.EntityDisplayComponent("minecraft:minecart", "entity.minecraft.minecart", identity)

	c.MoveTo(3.5, 64, 2.5, 0, 60)
	c.Settle()
	c.UseOn(cartID)
	if riders := servertest.AwaitPassengersFor(c, cartID); len(riders) != 1 {
		t.Fatalf("the cart holds %v, want the rider aboard", riders)
	}

	// The flags the twin has to carry across.
	c.ChatCommand("data merge entity @e[type=minecraft:minecart,limit=1] {NoGravity:1b,FlippedRotation:1b}")
	servertest.AwaitCommandFeedbackText(t, c, "commands.data.entity.modified", display)

	// The cart itself is the target, so the rider crosses seated on the
	// twin the command makes.
	c.ChatCommand("execute as @e[type=minecraft:minecart] in minecraft:the_nether run tp @s 8 40 8")
	// The rider crosses before the twin is registered and reseated, so wait
	// for the seat to be taken, not only for the rider's own dimension.
	servertest.PollUntil(t, 20*time.Second, func() bool {
		nether := srv.MinecartsInDimension(world.NetherName)
		if len(nether) != 1 {
			return false
		}
		_, seated := srv.MinecartRider(nether[0].ID)
		return seated
	})
	nether := srv.MinecartsInDimension(world.NetherName)
	if len(nether) != 1 {
		t.Fatalf("minecarts in the nether = %d, want 1", len(nether))
	}
	if nether[0].UUID != before[0].UUID {
		t.Errorf("the crossed cart's identity = %v, want the cart's %v", nether[0].UUID, before[0].UUID)
	}
	if got := srv.MinecartsInDimension(world.DimensionName); len(got) != 0 {
		t.Errorf("minecarts left in the overworld = %d, want none", len(got))
	}
	if rider, ok := srv.MinecartRider(nether[0].ID); !ok || rider != "Ferryman" {
		t.Errorf("the crossed cart's rider = %q (%v), want Ferryman still aboard", rider, ok)
	}
	if riders := srv.MinecartPassengerIDs(nether[0].ID); len(riders) != 1 {
		t.Errorf("passengers after the crossing = %v, want the rider remounted", riders)
	}
	// The nether's stream read through, so the server is not left holding
	// chunks nobody is reading.
	if _, ok := servertest.ReadUntil(t, c, servertest.ExpectTimeout, protocol.IDPlayRespawn); !ok {
		t.Fatal("no respawn arrived for the carried crossing")
	}
	if _, ok := servertest.ReadUntilViewSent(t, c); !ok {
		t.Fatal("the nether never arrived around the crossing")
	}

	// The twin kept the flags the write turned.
	c.ChatCommand("data get entity @e[type=minecraft:minecart,limit=1] NoGravity")
	servertest.AwaitCommandFeedbackText(t, c, "commands.data.entity.query",
		display, servertest.FeedbackArgsForTest("commands.data.entity.query", []string{"1b"})[0])
	c.ChatCommand("data get entity @e[type=minecraft:minecart,limit=1] FlippedRotation")
	servertest.AwaitCommandFeedbackText(t, c, "commands.data.entity.query",
		display, servertest.FeedbackArgsForTest("commands.data.entity.query", []string{"1b"})[0])
}

// TestATeleportedCartLeavesATravellingRiderBehind: a rider whose own
// crossing is already in flight is carried by that crossing, not by a cart
// teleported under them, so the cart's new body must not take a seat they
// would never reach.
func TestATeleportedCartLeavesATravellingRiderBehind(t *testing.T) {
	t.Parallel()
	cfg := servertest.BlockTestConfig()
	srv, c := servertest.Join(t, cfg, "Ferryman", servertest.OpsListing(t, "Ferryman"))

	servertest.CartRails(t, srv, 3, 6)
	cartID := srv.SpawnMinecart(3.5, 64.0625, 1.5)
	if cartID == 0 {
		t.Fatal("SpawnMinecart came to nothing")
	}
	c.MoveTo(3.5, 64, 2.5, 0, 60)
	c.Settle()
	c.UseOn(cartID)
	if riders := servertest.AwaitPassengersFor(c, cartID); len(riders) != 1 {
		t.Fatalf("the cart holds %v, want the rider aboard", riders)
	}

	// The window opens with the cart's riders already copied; the rider is
	// put into a crossing of their own while the cart crosses beneath them.
	srv.CartCrossBetween(func() { srv.RideOutOfCart("Ferryman") })
	// With no one in the nether the cart's chunk would unload and take the
	// cart out of the count the test reads.
	srv.RunLine("execute in minecraft:the_nether run forceload add 8 8")
	c.ChatCommand("execute as @e[type=minecraft:minecart] in minecraft:the_nether run tp @s 8 40 8")

	servertest.PollUntil(t, 20*time.Second, func() bool {
		return len(srv.MinecartsInDimension(world.NetherName)) == 1
	})
	nether := srv.MinecartsInDimension(world.NetherName)
	if len(nether) != 1 {
		t.Fatalf("minecarts in the nether = %d, want 1", len(nether))
	}
	if rider, ok := srv.MinecartRider(nether[0].ID); ok {
		t.Errorf("the crossed cart's rider = %q, want the travelling rider left behind", rider)
	}
	if riders := srv.MinecartPassengerIDs(nether[0].ID); len(riders) != 0 {
		t.Errorf("passengers after the crossing = %v, want none", riders)
	}
	if got := srv.PlayerDimension("Ferryman"); got != world.DimensionName {
		t.Errorf("the travelling rider's dimension = %q, want %q", got, world.DimensionName)
	}
}
