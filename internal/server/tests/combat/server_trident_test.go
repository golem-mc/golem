package combat

import (
	"iter"
	"testing"

	"github.com/golem-mc/golem/internal/config"
	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/internal/server"
	"github.com/golem-mc/golem/internal/server/rules"
	"github.com/golem-mc/golem/internal/server/servertest"
	"github.com/golem-mc/golem/internal/server/world"
	"github.com/golem-mc/golem/nbt"
	"github.com/golem-mc/golem/registry"
	"github.com/google/uuid"
)

// The trident over the wire: the charge a Use Item starts, the throw the
// release lets go, the hit of eight the thrown trident deals a cow, the
// stick its bounce or a wall ends, the pickup its owner alone may take,
// and the return a loyalty trident makes. Everything runs against real
// connections the way the bow's tests run, on the flat world the harness
// grows, at the easy difficulty the bow's tests play on.

// tridentTicks bounds a wait for a trident's doing in ticks of the held
// loop: the ticks of a return are few, so 600 is only a
// ceiling for a failing run.
const tridentTicks = 600

// tridentPackets yields the play packets c receives while the next n ticks
// run; breaking out of the loop stops the ticks.
func tridentPackets(c *servertest.Client, srv *server.Server, n int) iter.Seq2[int32, []byte] {
	return func(yield func(int32, []byte) bool) {
		c.T.Helper()
		c.ReadTicks(srv, n, func(id int32, data []byte) bool { return !yield(id, data) })
	}
}

// tridentSound is one Sound Effect packet the trident tests care about:
// the registry ID of the sound it played and the volume it played at.
type tridentSound struct {
	id     int32
	volume float32
}

// readTridentSound reads a Sound Effect packet's sound ID and volume,
// over the holder, the category and the position.
func readTridentSound(c *servertest.Client, data []byte) tridentSound {
	c.T.Helper()
	r := protocol.NewReader(data)
	s := tridentSound{}
	if holder := r.ReadVarInt(); holder == 0 {
		// The inline definition, which none of the trident's sounds is.
		r.ReadString(32767)
		r.ReadBool()
	} else {
		s.id = holder - 1
	}
	r.ReadVarInt() // category
	r.ReadI32()
	r.ReadI32()
	r.ReadI32()
	s.volume = r.ReadF32()
	if err := r.Err(); err != nil {
		c.T.Fatalf("sound effect: %v", err)
	}
	return s
}

// tridentSoundOf is the registry ID of a named sound, loud where the
// sound registry has none.
func tridentSoundOf(t *testing.T, name string) int32 {
	t.Helper()
	id, ok := registry.SoundEventID(name)
	if !ok {
		t.Fatalf("the sound registry has no %s", name)
	}
	return id
}

// TestAStuckTridentSurvivesARestart stands a spent thrown trident in the
// ground and takes the world down: on disk the compound is the thrown
// trident's own — the trident's id, its stack as the vanilla item, the
// blow it dealt and the eight it fights with — and the restarted world
// stands it back up as the trident it was, in the ground, its loyalty and
// its wear intact.
func TestAStuckTridentSurvivesARestart(t *testing.T) {
	t.Parallel()
	cfg := servertest.SurvivalConfig()
	cfg.LevelName = t.TempDir()

	srv, c := servertest.Join(t, cfg, "Keeper")

	const (
		tridentX = 14.5
		tridentY = 64.0
		tridentZ = 14.5
	)
	srv.SpawnStuckTrident(tridentX, tridentY, tridentZ, 2, 1)
	thrown := c.AwaitSpawn(protocol.EntityTypeTrident)

	srv.FlushMobs()
	srv.Shutdown()

	// On disk the compound is the vanilla one: the trident's own id, its
	// stack under "item" — the throw's wear in its components — the blow
	// it dealt kept, and the eight the trident fights with.
	servertest.EntityFileIs(t, cfg.LevelName, world.ChunkPos{X: 0, Z: 0}, "minecraft:trident", func(compound nbt.Compound) {
		if inGround, ok := nbt.Get[nbt.Byte](compound, "inGround"); !ok || inGround == 0 {
			t.Errorf("inGround = %v, %v; want 1", inGround, ok)
		}
		if dealt, ok := nbt.Get[nbt.Byte](compound, "DealtDamage"); !ok || dealt != 1 {
			t.Errorf("DealtDamage = %v, %v; want 1, the spent blow held", dealt, ok)
		}
		if damage, ok := nbt.Get[nbt.Double](compound, "damage"); !ok || float64(damage) != 2.0 {
			t.Errorf("damage = %v, %v; want the arrow's own 2.0 the trident never changes", damage, ok)
		}
		item, ok := nbt.Get[nbt.Compound](compound, "item")
		if !ok {
			t.Fatal("the thrown stack is missing from the file")
		}
		if id, _ := nbt.Get[nbt.String](item, "id"); id != "minecraft:trident" {
			t.Errorf("the thrown stack's id = %v, want the trident", id)
		}
		if count, _ := nbt.Get[nbt.Int](item, "count"); count != 1 {
			t.Errorf("the thrown stack's count = %v, want the one a spent throw is", count)
		}
		components, _ := nbt.Get[nbt.Compound](item, "components")
		if wear, _ := nbt.Get[nbt.Int](components, "minecraft:damage"); wear != 1 {
			t.Errorf("the thrown stack's wear = %v, want the throw's single point", wear)
		}
	})

	// And the restarted server stands it back up as a trident: the same
	// identity, in the ground where it was, its loyalty told of again.
	restarted := *cfg
	_, reconnect := servertest.Start(t, &restarted)
	again := reconnect()
	again.Login("Keeper")
	again.Configure()
	shown := again.JoinCapturing("Keeper", map[int32]string{
		protocol.EntityTypeTrident: "trident",
	})
	back := servertest.CapturedOr(t, again, shown, "trident", protocol.EntityTypeTrident)
	servertest.ExpectUUID(t, back.Uuid, thrown.Uuid, "trident")
	if back.X != tridentX || back.Y != tridentY || back.Z != tridentZ {
		t.Errorf("the trident came back at (%g, %g, %g), want (%g, %g, %g)",
			back.X, back.Y, back.Z, tridentX, tridentY, tridentZ)
	}
	if back.MetaBad {
		t.Fatal("the restored trident's metadata could not be read")
	}
	if entry := servertest.MetaOf(back.Meta, protocol.MetadataIndexArrowInGround); entry == nil || !entry.FlagVal {
		t.Error("the restored trident was not shown in the ground")
	}
	if entry := servertest.MetaOf(back.Meta, protocol.MetadataIndexTridentLoyalty); entry == nil || entry.ByteVal != 2 {
		t.Errorf("the restored trident's loyalty byte = %v, want the two of its stack", entry)
	}
	// A load never sets the glint, so the enchanted stack's is not told.
	// vanilla: ThrownTrident.readAdditionalSaveData
	if entry := servertest.MetaOf(back.Meta, protocol.MetadataIndexTridentFoil); entry != nil {
		t.Errorf("the restored trident's enchantment glint was told %v, want it left unset", entry.FlagVal)
	}
}

// TestADeadOwnersTridentFallsWhereItStands throws a loyalty trident into a
// wall, waits it stuck, and kills its owner with a fall: the return has no
// owner to come home to — no acceptable one, the dead being none — and the
// stack the throw spent falls where the trident stands, the body gone from
// the world rather than orbiting a corpse.
func TestADeadOwnersTridentFallsWhereItStands(t *testing.T) {
	t.Parallel()
	srv, c, _ := servertest.JoinAs(t, servertest.BowTestConfig(), "Thrower")
	giveLoyalTrident(srv, "Thrower", 1)

	stone, ok := registry.FindState("minecraft:stone", nil)
	if !ok {
		t.Fatal("the block registry has no stone")
	}
	for y := int32(64); y <= 66; y++ {
		if !srv.SetBlock(4, y, 0, stone) {
			t.Fatalf("could not place the wall at (4, %d, 0)", y)
		}
	}

	c.MoveTo(0.5, 64, 0.5, -90, 0)
	startTridentCharge(t, srv, c, "Thrower", -90, 0, 12)
	endTridentCharge(t, srv, c, "Thrower")

	trident := expectTridentSpawn(c, srv)
	if !waitForTridentInGround(c, srv, trident.entityID) {
		t.Fatal("the loyal trident never came to rest in the wall")
	}
	// The fall tall enough to be the end, settled before the held loop
	// runs another tick: five stuck ticks make the blow dealt, and a dealt
	// loyalty trident whose owner still lives flies home and is taken,
	// so the corpse would drop it instead.
	c.MoveTo(10.5, 126, 10.5, -90, 0)
	c.MoveToGround(10.5, 64, 10.5, true)
	c.Settle()
	servertest.UntilTicks(t, srv, 1200, "condition", func() bool { return srv.PlayerHealth("Thrower") <= 0 })
	// The return falls apart: the stack lies where the body stood, and
	// the trident is gone.
	servertest.UntilTicks(t, srv, 1200, "condition", func() bool { return !srv.ArrowAlive(trident.entityID) })
	// The stack is laid after the body is taken, so a read between the two
	// would find neither; a tick is given for it.
	servertest.WaitForTicks(t, srv, 2, func() bool {
		for _, item := range srv.ItemsInDimension(world.DimensionName) {
			if item.Item == "minecraft:trident" && item.Count == 1 {
				return true
			}
		}
		return false
	}, "the dead owner's trident never fell as the stack it was")
}

// TestASpectatorsTridentFallsWhereItStands throws a loyalty trident into a
// wall, waits it stuck, and slips its owner into spectator: no acceptable
// owner, the spectators being none, so the return falls apart and the stack
// the throw spent lies where the body stands.
func TestASpectatorsTridentFallsWhereItStands(t *testing.T) {
	t.Parallel()
	srv, c, _ := servertest.JoinAs(t, servertest.BowTestConfig(), "Thrower", servertest.OpsListing(t, "Thrower"))
	giveLoyalTrident(srv, "Thrower", 1)

	stone, ok := registry.FindState("minecraft:stone", nil)
	if !ok {
		t.Fatal("the block registry has no stone")
	}
	for y := int32(64); y <= 66; y++ {
		if !srv.SetBlock(4, y, 0, stone) {
			t.Fatalf("could not place the wall at (4, %d, 0)", y)
		}
	}

	c.MoveTo(0.5, 64, 0.5, -90, 0)
	startTridentCharge(t, srv, c, "Thrower", -90, 0, 12)
	endTridentCharge(t, srv, c, "Thrower")

	trident := expectTridentSpawn(c, srv)
	if !waitForTridentInGround(c, srv, trident.entityID) {
		t.Fatal("the loyal trident never came to rest in the wall")
	}
	// The owner leaves the world for the spectator's: the trident waiting
	// on a return has no acceptable body to come home to.
	// Settled before any tick runs: five stuck ticks make the blow dealt,
	// and a dealt loyalty trident whose owner is still in the world flies
	// home and is taken instead of falling.
	c.ChatCommand("gamemode spectator Thrower")
	c.Settle()
	servertest.UntilTicks(t, srv, 1200, "condition", func() bool { return srv.PlayerGameMode("Thrower") == protocol.GameModeSpectator })
	// The return falls apart: the stack lies where the body stands, and
	// the trident is gone.
	servertest.UntilTicks(t, srv, 1200, "condition", func() bool { return !srv.ArrowAlive(trident.entityID) })
	// The stack is laid after the body is taken, so a read between the two
	// would find neither; a tick is given for it.
	servertest.WaitForTicks(t, srv, 2, func() bool {
		for _, item := range srv.ItemsInDimension(world.DimensionName) {
			if item.Item == "minecraft:trident" && item.Count == 1 {
				return true
			}
		}
		return false
	}, "the spectator owner's trident never fell as the stack it was")
}

// giveTrident puts a trident into a player's hotbar the way /give would.
func giveTrident(srv *server.Server, name string) {
	srv.GiveSlot(name, 36, protocol.ItemStack{ItemID: servertest.MustItem("minecraft:trident"), Count: 1})
}

// giveLoyalTrident puts a trident carrying loyalty of the level given
// into a player's hotbar, the stack an enchanted table might have left.
func giveLoyalTrident(srv *server.Server, name string, level int32) {
	id, ok := registry.EnchantmentID("minecraft:loyalty")
	if !ok {
		panic("the enchantment registry has no loyalty")
	}
	srv.GiveSlot(name, 36, protocol.ItemStack{
		ItemID:       servertest.MustItem("minecraft:trident"),
		Count:        1,
		Enchantments: map[int32]int32{id: level},
	})
}

// giveRiptideTrident puts a trident carrying riptide of the level given
// into a player's hotbar, the spin-attack stack that is never thrown.
func giveRiptideTrident(srv *server.Server, name string, level int32) {
	id, ok := registry.EnchantmentID("minecraft:riptide")
	if !ok {
		panic("the enchantment registry has no riptide")
	}
	srv.GiveSlot(name, 36, protocol.ItemStack{
		ItemID:       servertest.MustItem("minecraft:trident"),
		Count:        1,
		Enchantments: map[int32]int32{id: level},
	})
}

// startTridentCharge begins the trident's charge the same Use Item packet
// a bow's draw begins, aimed where the yaw and pitch say, and counts its
// ticks out in hand-driven ticks so a running loop cannot run past the
// charge the test means. It leaves the loop stopped and the draw held, so
// the release ends it exactly as the test means to.
func startTridentCharge(t *testing.T, srv *server.Server, c *servertest.Client, name string, yaw, pitch float32, ticks int32) {
	t.Helper()
	c.Settle()
	srv.StopTick()
	servertest.DrawBow(c, yaw, pitch)
	c.Settle()
	for i := int32(0); srv.BowTicks(name) < ticks; i++ {
		if i == ticks+5 {
			t.Fatalf("%d hand ticks charged the trident %d ticks, want %d", ticks+5, srv.BowTicks(name), ticks)
		}
		srv.TickSurvival()
	}
}

// endTridentCharge lets the dash go the same release-use action a bow's
// release is and starts the tick loop again once the charge is spent.
func endTridentCharge(t *testing.T, srv *server.Server, c *servertest.Client, name string) {
	t.Helper()
	servertest.ReleaseBow(c)
	c.SettleKeeping()
	if got := srv.BowTicks(name); got != 0 {
		t.Fatalf("the release left the draw at %d ticks, want 0", got)
	}
	servertest.HoldTicks(t, srv)
}

// spawnedTrident is one thrown trident as the wire has it arrive: its
// entity ID, where it was born and how fast it flies, and the thrower its
// data field names.
type spawnedTrident struct {
	entityID   int32
	x, y, z    float64
	vx, vy, vz float64
	yaw, pitch float32
	ownerID    int32
}

// readTridentSpawn reads one Spawn Entity packet of the trident type,
// checking it against this repository's own field layout, leftover bytes
// and all.
func readTridentSpawn(c *servertest.Client, data []byte) spawnedTrident {
	c.T.Helper()
	r := protocol.NewReader(data)
	t := spawnedTrident{}
	t.entityID = r.ReadVarInt()
	if t.entityID == 0 {
		c.T.Fatal("trident spawn ID = 0, want a real entity ID")
	}
	if r.ReadUUID() == uuid.Nil {
		c.T.Error("trident spawn UUID = nil, want the one it was spawned as")
	}
	if got := r.ReadVarInt(); got != protocol.EntityTypeTrident {
		c.T.Fatalf("spawned entity type = %d, want %d (the trident)", got, protocol.EntityTypeTrident)
	}
	t.x, t.y, t.z = r.ReadF64(), r.ReadF64(), r.ReadF64()
	t.vx, t.vy, t.vz = r.ReadLpVec3()
	t.pitch = r.ReadAngle()
	t.yaw = r.ReadAngle()
	r.ReadAngle() // head yaw, which a projectile does not carry
	t.ownerID = r.ReadVarInt()
	if err := r.Err(); err != nil {
		c.T.Fatalf("spawn entity (trident): %v", err)
	}
	if r.Remaining() != 0 {
		c.T.Errorf("spawn entity (trident) has %d bytes left over", r.Remaining())
	}
	return t
}

// expectTridentSpawn waits for the next trident to come into the world and
// reads its spawn whole, stepping over the traffic the charge and the
// other bodies carry.
func expectTridentSpawn(c *servertest.Client, srv *server.Server) spawnedTrident {
	c.T.Helper()
	for gotID, data := range tridentPackets(c, srv, tridentTicks) {
		switch gotID {
		case protocol.IDPlaySpawnEntity:
			r := protocol.NewReader(data)
			r.ReadVarInt()
			r.ReadUUID()
			if r.ReadVarInt() == protocol.EntityTypeTrident {
				return readTridentSpawn(c, data)
			}
		case protocol.IDPlayEntityMetadata, protocol.IDPlaySoundEffect,
			protocol.IDPlayEntityAnimation, protocol.IDPlayContainerSlot:
			// The charge's own traffic and the other bodies' states; read
			// past it.
		}
	}
	c.T.Fatal("no trident came into the world")
	return spawnedTrident{}
}

// tridentMetadata is what a trident's metadata says, read from a Set
// Entity Metadata packet about one: the noclip bit of the arrow flags,
// the in-ground flag, the loyalty level, and the enchantment glint, where
// they were told at all.
type tridentMetadata struct {
	entityID int32
	noclip   bool
	inGround bool
	loyalty  byte
	foil     bool
}

// readTridentMetadata reads a Set Entity Metadata packet's trident fields,
// over both the shared arrow's and the trident's own indices.
func readTridentMetadata(c *servertest.Client, data []byte) tridentMetadata {
	c.T.Helper()
	r := protocol.NewReader(data)
	m := tridentMetadata{entityID: r.ReadVarInt()}
	for r.Err() == nil {
		index := r.ReadU8()
		if index == protocol.MetadataEnd {
			break
		}
		switch fieldType := r.ReadVarInt(); index {
		case protocol.MetadataIndexArrowFlags:
			if fieldType != protocol.MetadataByte {
				c.T.Fatalf("arrow flags field type = %d, want %d (a byte)", fieldType, protocol.MetadataByte)
			}
			m.noclip = r.ReadU8()&protocol.EntityFlagArrowNoClip != 0
		case protocol.MetadataIndexArrowInGround:
			if fieldType != protocol.MetadataBoolean {
				c.T.Fatalf("in-ground field type = %d, want %d (a boolean)", fieldType, protocol.MetadataBoolean)
			}
			m.inGround = r.ReadBool()
		case protocol.MetadataIndexTridentLoyalty:
			if fieldType != protocol.MetadataByte {
				c.T.Fatalf("loyalty field type = %d, want %d (a byte)", fieldType, protocol.MetadataByte)
			}
			m.loyalty = r.ReadU8()
		case protocol.MetadataIndexTridentFoil:
			if fieldType != protocol.MetadataBoolean {
				c.T.Fatalf("foil field type = %d, want %d (a boolean)", fieldType, protocol.MetadataBoolean)
			}
			m.foil = r.ReadBool()
		default:
			c.T.Fatalf("unexpected trident metadata field %d of type %d", index, fieldType)
		}
	}
	if err := r.Err(); err != nil {
		c.T.Fatalf("trident metadata: %v", err)
	}
	if r.Remaining() != 0 {
		c.T.Errorf("trident metadata has %d bytes left over", r.Remaining())
	}
	return m
}

// waitForTridentInGround waits for the trident's own metadata to say it
// is in the ground.
func waitForTridentInGround(c *servertest.Client, srv *server.Server, tridentID int32) bool {
	c.T.Helper()
	for gotID, data := range tridentPackets(c, srv, tridentTicks) {
		if gotID != protocol.IDPlayEntityMetadata {
			continue
		}
		if protocol.NewReader(data).ReadVarInt() != tridentID {
			continue
		}
		if m := readTridentMetadata(c, data); m.inGround {
			return true
		}
	}
	return false
}

// tridentsHeld counts the tridents a named player carries across the 46
// slots of their own window, and reports the stack of the first it finds.
func tridentsHeld(srv *server.Server, name string) (count int32, stack protocol.ItemStack) {
	trident := servertest.MustItem("minecraft:trident")
	for slot := range 46 {
		held := srv.InventorySlot(name, slot)
		if held.ItemID == trident {
			count += held.Count
			if stack.Empty() {
				stack = held
			}
		}
	}
	return count, stack
}

// TestAChargedTridentThrowSpendsTheHeldStack draws a trident for the
// charge the throw asks and lets go: the trident of the trident type comes
// into the world owned by the thrower — the spawn's data field naming them
// — born at the thrower's eye less its tenth and flying flat at the
// throw's own pace, with the stack left the hand it was spent from.
func TestAChargedTridentThrowSpendsTheHeldStack(t *testing.T) {
	t.Parallel()
	srv, c, playerID := servertest.JoinAs(t, servertest.BowTestConfig(), "Thrower")
	giveTrident(srv, "Thrower")

	c.MoveTo(0.5, 64, 0.5, -90, 0)
	startTridentCharge(t, srv, c, "Thrower", -90, 0, 12)
	endTridentCharge(t, srv, c, "Thrower")

	trident := expectTridentSpawn(c, srv)
	if trident.ownerID != playerID {
		t.Errorf("the trident's data field names entity %d, want the thrower's %d", trident.ownerID, playerID)
	}
	// The throw leaves at the 2.5 of TridentItem's own shot: level, the
	// spread of the inaccuracy one apart.
	speed := trident.vx*trident.vx + trident.vy*trident.vy + trident.vz*trident.vz
	if speed < 5 || speed > 7 {
		t.Errorf("the trident flies at %v blocks a tick squared, want the 2.5 of the throw's velocity", speed)
	}
	if trident.vy > 0.1 || trident.vy < -0.1 {
		t.Errorf("a level throw's rise = %v, want the flat of a level look", trident.vy)
	}
	// The birth: at the thrower's eye less its tenth, over their feet.
	if d := trident.y - (64 + 1.62 - 0.1); d < -0.2 || d > 0.2 {
		t.Errorf("the trident was born at y=%v, want the thrower's eye less its tenth", trident.y)
	}
	// The throw spends the stack: the trident is gone from the hotbar and
	// nowhere else in the window.
	if count, _ := tridentsHeld(srv, "Thrower"); count != 0 {
		t.Errorf("the thrower holds %d tridents after the throw, want none", count)
	}
}

// TestAShortTridentDrawThrowsNothing draws a trident for less than the
// ten ticks a charge takes and lets go: no trident comes into the world,
// and the drawn stack is left in the hand it was held in.
func TestAShortTridentDrawThrowsNothing(t *testing.T) {
	t.Parallel()
	srv, c, _ := servertest.JoinAs(t, servertest.BowTestConfig(), "Thrower")
	giveTrident(srv, "Thrower")

	c.MoveTo(0.5, 64, 0.5, -90, 0)
	startTridentCharge(t, srv, c, "Thrower", -90, 0, 6)
	endTridentCharge(t, srv, c, "Thrower")

	for gotID, data := range tridentPackets(c, srv, 40) {
		if gotID != protocol.IDPlaySpawnEntity {
			continue
		}
		r := protocol.NewReader(data)
		r.ReadVarInt()
		r.ReadUUID()
		if got := r.ReadVarInt(); got == protocol.EntityTypeTrident {
			t.Fatalf("a draw of six ticks threw a trident anyway")
		}
	}
	if count, stack := tridentsHeld(srv, "Thrower"); count != 1 || stack.Damage != 0 {
		t.Errorf("the undrawn trident = %d of damage %d, want the one undamaged the short draw left", count, stack.Damage)
	}
}

// TestATridentDrawnNineElapsedTicksThrowsNothing releases a trident after
// nine elapsed ticks, one short of the ten a throw needs.
func TestATridentDrawnNineElapsedTicksThrowsNothing(t *testing.T) {
	t.Parallel()
	cfg := servertest.BowTestConfig()
	cfg.ViewDistance, cfg.SimulationDistance = config.MinDistance, config.MinDistance
	srv, c, _ := servertest.JoinAs(t, cfg, "Thrower")
	giveTrident(srv, "Thrower")

	c.MoveTo(0.5, 64, 0.5, -90, 0)
	startTridentCharge(t, srv, c, "Thrower", -90, 0, 10)
	endTridentCharge(t, srv, c, "Thrower")

	c.ExpectNoneWithinTicks(srv, 0, func(id int32, data []byte) bool {
		if id != protocol.IDPlaySpawnEntity {
			return false
		}
		r := protocol.NewReader(data)
		r.ReadVarInt()
		r.ReadUUID()
		return r.ReadVarInt() == protocol.EntityTypeTrident
	})
}

// TestAThrownTridentHurtsACowAndBouncesOff charges a trident at a cow four
// blocks off and lets go: the cow takes the trident's eight — both its
// half-hearts left of the ten it had — the blow named the trident's own
// damage type, and the thrown body itself bounces off instead of spending
// itself the way an arrow does: its flight turning back, still in the
// world, and the hit's own voice sounding.
func TestAThrownTridentHurtsACowAndBouncesOff(t *testing.T) {
	t.Parallel()
	srv, c, playerID := servertest.JoinAs(t, servertest.BowTestConfig(), "Thrower")
	giveTrident(srv, "Thrower")

	cowID := srv.SpawnMob("minecraft:cow", 10.5, 64, 0.5)
	cow := c.ExpectMobSpawn(protocol.EntityTypeCow)
	if cow.EntityID != cowID {
		t.Fatalf("spawned cow's entity ID = %d, want the one SpawnMob reported (%d)", cow.EntityID, cowID)
	}

	// The look dips into the cow, the twelfth of a circle carrying the
	// throw onto the body four blocks off.
	c.MoveTo(0.5, 64, 0.5, -90, 12)
	startTridentCharge(t, srv, c, "Thrower", -90, 12, 12)
	srv.MoveMob(cowID, 4.5, 0.5)
	endTridentCharge(t, srv, c, "Thrower")

	trident := expectTridentSpawn(c, srv)
	var (
		health     float32
		damageSeen bool
		bounced    bool
		hitSound   bool
		lived      bool
	)
	for gotID, data := range tridentPackets(c, srv, tridentTicks) {
		switch gotID {
		case protocol.IDPlayEntityMetadata:
			r := protocol.NewReader(data)
			if id := r.ReadVarInt(); id != cowID {
				continue
			}
			for r.Err() == nil {
				index := r.ReadU8()
				if index == protocol.MetadataEnd {
					break
				}
				if fieldType := r.ReadVarInt(); index == protocol.MetadataIndexLivingHealth && fieldType == protocol.MetadataFloat {
					health = r.ReadF32()
				}
			}
		case protocol.IDPlayDamageEvent:
			r := protocol.NewReader(data)
			if got := r.ReadVarInt(); got != cowID {
				continue
			}
			if got := r.ReadVarInt(); got != registry.MustID("minecraft:damage_type", rules.DamageTypeTrident.Name()) {
				t.Errorf("the blow's damage type = %d, want the trident's own", got)
				continue
			}
			if got := r.ReadVarInt(); got != playerID+1 {
				t.Errorf("the blow's cause = %d, want the thrower behind it (%d)", got, playerID+1)
			}
			if got := r.ReadVarInt(); got != trident.entityID+1 {
				t.Errorf("the blow's direct = %d, want the trident itself (%d)", got, trident.entityID+1)
			}
			damageSeen = true
		case protocol.IDPlaySetEntityMotion:
			// The flight runs east; a motion packet whose easting has
			// turned about is the bounce of the hit.
			r := protocol.NewReader(data)
			if r.ReadVarInt() == trident.entityID {
				if vx, _, _ := r.ReadLpVec3(); vx < 0 {
					bounced = true
				}
			}
		case protocol.IDPlaySoundEffect:
			if sound := readTridentSound(c, data); sound.id == tridentSoundOf(t, "minecraft:item.trident.hit") {
				hitSound = true
			}
		case protocol.IDPlayRemoveEntities:
			for _, id := range servertest.RemovedIDs(c.T, data) {
				if id == trident.entityID {
					t.Fatal("the trident that hit the cow was taken out of the world")
				}
			}
		}
		if health == 2 && damageSeen && bounced && hitSound {
			lived = true
			break
		}
	}
	if !lived {
		t.Fatalf("the cow's health = %v, damage event %v, bounce %v, hit sound %v: the blow never came whole", health, damageSeen, bounced, hitSound)
	}
	if !srv.ArrowAlive(trident.entityID) {
		t.Error("the trident that bounced off the cow was taken out of the world")
	}
}

// TestAStuckTridentIsOnlyItsOwnersToPickUp throws a trident into a wall
// and walks a stranger over the stuck body first: the thrower alone may
// pick the trident back up, whoever else walks over it — the one the
// walkover takes is the stack the throw spent, the throw's single point
// of wear in it. The stranger's walk leaves the trident where it stuck.
func TestAStuckTridentIsOnlyItsOwnersToPickUp(t *testing.T) {
	t.Parallel()
	srv, connect := servertest.Start(t, servertest.BowTestConfig())
	owner := connect()
	playerID := owner.JoinAs("Thrower")
	giveTrident(srv, "Thrower")

	stranger := connect()
	strangerID := stranger.JoinAs("Walker")
	// Both spawn at the world's spawn point; the stranger steps clear of
	// the throw line before the throw, or the trident flying out of it
	// would take a body standing in it.
	stranger.MoveTo(-6.5, 64, 6.5, 0, 0)

	stone, ok := registry.FindState("minecraft:stone", nil)
	if !ok {
		t.Fatal("the block registry has no stone")
	}
	for y := int32(64); y <= 66; y++ {
		if !srv.SetBlock(4, y, 0, stone) {
			t.Fatalf("could not place the wall at (4, %d, 0)", y)
		}
	}

	owner.MoveTo(0.5, 64, 0.5, -90, 0)
	startTridentCharge(t, srv, owner, "Thrower", -90, 0, 12)
	endTridentCharge(t, srv, owner, "Thrower")

	trident := expectTridentSpawn(owner, srv)
	if !waitForTridentInGround(owner, srv, trident.entityID) {
		t.Fatal("the trident never came to rest in the wall")
	}

	// The stranger walks over the stuck trident and stands about it: the
	// five seconds of a few pickup passes take nothing.
	stranger.MoveTo(3.4, 64, 0.5, -90, 0)
	for gotID, data := range tridentPackets(stranger, srv, 100) {
		switch gotID {
		case protocol.IDPlayPickupItem:
			collected, collector, _ := servertest.ReadPickupItem(stranger, data)
			if collector == strangerID && collected == trident.entityID {
				t.Fatal("a stranger picked the stuck trident up")
			}
		case protocol.IDPlayRemoveEntities:
			for _, id := range servertest.RemovedIDs(stranger.T, data) {
				if id == trident.entityID {
					t.Fatalf("the stuck trident left the world while only a stranger stood over it (id %d)", id)
				}
			}
		}
	}
	if !srv.ArrowAlive(trident.entityID) {
		t.Fatal("the stuck trident vanished while only a stranger stood over it")
	}

	// The thrower walks over it: the trident is theirs, and comes back
	// whole — the stack it was, one throw's wear the older.
	owner.MoveTo(3.4, 64, 0.5, -90, 0)
	picked, removed := false, false
	for gotID, data := range tridentPackets(owner, srv, tridentTicks) {
		switch gotID {
		case protocol.IDPlayPickupItem:
			collected, collector, _ := servertest.ReadPickupItem(owner, data)
			if collected == trident.entityID && collector == playerID {
				picked = true
			}
		case protocol.IDPlayRemoveEntities:
			for _, id := range servertest.RemovedIDs(owner.T, data) {
				if id == trident.entityID {
					removed = true
				}
			}
		}
		if picked && removed {
			break
		}
	}
	if !picked || !removed {
		t.Fatalf("the thrower's walkover: pickup %v, removal %v, want both", picked, removed)
	}
	count, stack := tridentsHeld(srv, "Thrower")
	if count != 1 {
		t.Errorf("the thrower holds %d tridents after the pickup, want the one come back", count)
	}
	if stack.Damage != 1 {
		t.Errorf("the picked-up trident's damage = %d, want the one point the throw wore on it", stack.Damage)
	}
}

// TestALoyalTridentReturnsToItsThrower throws a trident carrying loyalty
// II into a wall and stands the thrower off it: once the trident has spent
// its flight it turns back, its return heard, and reaches the thrower
// whole — its enchantment with it — without a step being taken for it.
func TestALoyalTridentReturnsToItsThrower(t *testing.T) {
	t.Parallel()
	srv, c, ownerID := servertest.JoinAs(t, servertest.BowTestConfig(), "Thrower")
	giveLoyalTrident(srv, "Thrower", 2)

	stone, ok := registry.FindState("minecraft:stone", nil)
	if !ok {
		t.Fatal("the block registry has no stone")
	}
	for y := int32(64); y <= 66; y++ {
		if !srv.SetBlock(4, y, 0, stone) {
			t.Fatalf("could not place the wall at (4, %d, 0)", y)
		}
	}

	c.MoveTo(0.5, 64, 0.5, -90, 0)
	startTridentCharge(t, srv, c, "Thrower", -90, 0, 12)
	endTridentCharge(t, srv, c, "Thrower")

	trident := expectTridentSpawn(c, srv)
	// The pairing's metadata: the loyalty of the thrown stack — the level
	// two, the byte the entity syncs — and the enchantment glint of it.
	seen := false
	for gotID, data := range tridentPackets(c, srv, 1200) {
		if gotID != protocol.IDPlayEntityMetadata {
			continue
		}
		if protocol.NewReader(data).ReadVarInt() != trident.entityID {
			continue
		}
		m := readTridentMetadata(c, data)
		if m.loyalty != 2 {
			t.Errorf("the trident's loyalty metadata = %d, want the two of the thrown stack", m.loyalty)
		}
		if !m.foil {
			t.Error("the enchanted trident carries no glint metadata")
		}
		seen = true
		break
	}
	if !seen {
		t.Fatal("the trident's spawn metadata never arrived")
	}
	if !waitForTridentInGround(c, srv, trident.entityID) {
		t.Fatal("the loyal trident never came to rest in the wall")
	}

	// The thrower stands away from the wall and gives the trident a
	// clear field to come back over.
	c.MoveTo(2.5, 64, 1.5, -90, 0)
	var (
		returnSound bool
		picked      bool
		removed     bool
	)
	for gotID, data := range tridentPackets(c, srv, tridentTicks) {
		switch gotID {
		case protocol.IDPlaySoundEffect:
			sound := readTridentSound(c, data)
			if sound.id == tridentSoundOf(t, "minecraft:item.trident.return") {
				if sound.volume != 10 {
					t.Errorf("the return sound plays at volume %v, want the ten its reach carries", sound.volume)
				}
				returnSound = true
			}
		case protocol.IDPlayPickupItem:
			collected, collector, _ := servertest.ReadPickupItem(c, data)
			if collected == trident.entityID && collector == ownerID {
				picked = true
			}
		case protocol.IDPlayRemoveEntities:
			for _, id := range servertest.RemovedIDs(c.T, data) {
				if id == trident.entityID {
					removed = true
				}
			}
		}
		if returnSound && picked && removed {
			break
		}
	}
	if !returnSound {
		t.Fatal("the loyal trident's return was never heard")
	}
	if !picked || !removed {
		t.Fatalf("the returning trident came home %v and left the world %v: the pickup never finished", picked, removed)
	}
	count, stack := tridentsHeld(srv, "Thrower")
	if count != 1 {
		t.Errorf("the thrower holds %d tridents after the return, want the one come back", count)
	}
	if stack.Damage != 1 {
		t.Errorf("the returned trident's damage = %d, want the one point the throw wore on it", stack.Damage)
	}
	if level := stack.Enchantments[loyaltyID(t)]; level != 2 {
		t.Errorf("the returned trident carries loyalty %d, want the two it was thrown with", level)
	}
}

// loyaltyID is the numeric ID of the loyalty enchantment, which the
// returns read the level of the stack by.
func loyaltyID(t *testing.T) int32 {
	t.Helper()
	id, ok := registry.EnchantmentID("minecraft:loyalty")
	if !ok {
		t.Fatal("the enchantment registry has no loyalty")
	}
	return id
}

// TestALoyalTridentRuledDisallowedStillReturns throws a loyalty III
// trident at a wall and outlaws its pickup once it is stuck: the return
// still comes home to the thrower, its stack added rather than the body
// leaving the world with the stack the throw spent uncounted.
func TestALoyalTridentRuledDisallowedStillReturns(t *testing.T) {
	t.Parallel()
	srv, c, ownerID := servertest.JoinAs(t, servertest.BowTestConfig(), "Thrower")
	giveLoyalTrident(srv, "Thrower", 3)

	stone, ok := registry.FindState("minecraft:stone", nil)
	if !ok {
		t.Fatal("the block registry has no stone")
	}
	for y := int32(64); y <= 66; y++ {
		if !srv.SetBlock(4, y, 0, stone) {
			t.Fatalf("could not place the wall at (4, %d, 0)", y)
		}
	}

	c.MoveTo(0.5, 64, 0.5, -90, 0)
	startTridentCharge(t, srv, c, "Thrower", -90, 0, 12)
	endTridentCharge(t, srv, c, "Thrower")

	trident := expectTridentSpawn(c, srv)
	if !waitForTridentInGround(c, srv, trident.entityID) {
		t.Fatal("the loyal trident never came to rest in the wall")
	}
	// The throw spent the held stack; the rule now refuses the pickup the
	// return still makes. vanilla: ThrownTrident.tryPickup
	srv.StopTick()
	srv.SetArrowPickup(trident.entityID, "disallowed")
	srv.StartTick()

	var picked, removed bool
	for gotID, data := range tridentPackets(c, srv, tridentTicks) {
		switch gotID {
		case protocol.IDPlayPickupItem:
			collected, collector, _ := servertest.ReadPickupItem(c, data)
			if collected == trident.entityID && collector == ownerID {
				picked = true
			}
		case protocol.IDPlayRemoveEntities:
			for _, id := range servertest.RemovedIDs(c.T, data) {
				if id == trident.entityID {
					removed = true
				}
			}
		}
		if picked && removed {
			break
		}
	}
	if !picked || !removed {
		t.Fatalf("the disallowed return: pickup %v, removal %v, want both", picked, removed)
	}
	if count, _ := tridentsHeld(srv, "Thrower"); count != 1 {
		t.Errorf("the thrower holds %d tridents after the disallowed return, want the one come home", count)
	}
}

// TestACreativeThrowerKeepsTheTrident throws the trident a creative
// player charges with: the stack never leaves the hotbar, and the thrown
// body — a creative throw picks up only in creative — comes back to the
// same player it left without doubling anything.
func TestACreativeThrowerKeepsTheTrident(t *testing.T) {
	t.Parallel()
	cfg := config.Default()
	cfg.OnlineMode = false
	cfg.GameMode = "creative"
	srv, c, playerID := servertest.JoinAs(t, cfg, "Maker")
	giveTrident(srv, "Maker")

	stone, ok := registry.FindState("minecraft:stone", nil)
	if !ok {
		t.Fatal("the block registry has no stone")
	}
	for y := int32(64); y <= 66; y++ {
		if !srv.SetBlock(4, y, 0, stone) {
			t.Fatalf("could not place the wall at (4, %d, 0)", y)
		}
	}

	c.MoveTo(0.5, 64, 0.5, -90, 0)
	startTridentCharge(t, srv, c, "Maker", -90, 0, 12)
	endTridentCharge(t, srv, c, "Maker")

	trident := expectTridentSpawn(c, srv)
	if count, stack := tridentsHeld(srv, "Maker"); count != 1 || stack.Damage != 0 {
		t.Errorf("the creative thrower holds %d tridents of damage %d, want the one undamaged it never spent", count, stack.Damage)
	}
	if !waitForTridentInGround(c, srv, trident.entityID) {
		t.Fatal("the thrown trident never came to rest in the wall")
	}

	// The thrown body answers for the throw it was: picked up by the
	// creative thrower walking over where it lies, adding nothing — taken
	// out of the world empty.
	c.MoveTo(3.4, 64, 0.5, -90, 0)
	picked, removed := false, false
	for gotID, data := range tridentPackets(c, srv, tridentTicks) {
		switch gotID {
		case protocol.IDPlayPickupItem:
			collected, collector, _ := servertest.ReadPickupItem(c, data)
			if collected == trident.entityID && collector == playerID {
				picked = true
			}
		case protocol.IDPlayRemoveEntities:
			for _, id := range servertest.RemovedIDs(c.T, data) {
				if id == trident.entityID {
					removed = true
				}
			}
		}
		if picked && removed {
			break
		}
	}
	if !picked || !removed {
		t.Fatalf("the creative thrower's walkover: pickup %v, removal %v, want both", picked, removed)
	}
	// The creative take adds nothing: the hand never lost the stack, and
	// the walk gained none.
	if count, _ := tridentsHeld(srv, "Maker"); count != 1 {
		t.Errorf("the creative thrower holds %d tridents after the pickup, want the one never spent", count)
	}
}

// TestACreativeLoyalTridentComesHomeWithoutDoubling throws a loyalty
// trident the creative player keeps in hand and waits for the return: the
// returning stack is taken out of the world without a second one being
// added, the rule's creative-only answer beating the return.
func TestACreativeLoyalTridentComesHomeWithoutDoubling(t *testing.T) {
	t.Parallel()
	cfg := config.Default()
	cfg.OnlineMode = false
	cfg.GameMode = "creative"
	srv, c, playerID := servertest.JoinAs(t, cfg, "Maker")
	giveLoyalTrident(srv, "Maker", 3)

	stone, ok := registry.FindState("minecraft:stone", nil)
	if !ok {
		t.Fatal("the block registry has no stone")
	}
	for y := int32(64); y <= 66; y++ {
		if !srv.SetBlock(4, y, 0, stone) {
			t.Fatalf("could not place the wall at (4, %d, 0)", y)
		}
	}

	c.MoveTo(0.5, 64, 0.5, -90, 0)
	startTridentCharge(t, srv, c, "Maker", -90, 0, 12)
	endTridentCharge(t, srv, c, "Maker")

	trident := expectTridentSpawn(c, srv)
	if count, stack := tridentsHeld(srv, "Maker"); count != 1 || stack.Damage != 0 {
		t.Errorf("the creative thrower holds %d tridents of damage %d, want the one it never spent", count, stack.Damage)
	}
	if !waitForTridentInGround(c, srv, trident.entityID) {
		t.Fatal("the loyal trident never came to rest in the wall")
	}

	// The thrower stands off the wall and waits on the return home; the
	// stack it kept in hand must not be joined by a second.
	c.MoveTo(2.5, 64, 1.5, -90, 0)
	picked, removed := false, false
	for gotID, data := range tridentPackets(c, srv, tridentTicks) {
		switch gotID {
		case protocol.IDPlayPickupItem:
			collected, collector, _ := servertest.ReadPickupItem(c, data)
			if collected == trident.entityID && collector == playerID {
				picked = true
			}
		case protocol.IDPlayRemoveEntities:
			for _, id := range servertest.RemovedIDs(c.T, data) {
				if id == trident.entityID {
					removed = true
				}
			}
		}
		if picked && removed {
			break
		}
	}
	if !picked || !removed {
		t.Fatalf("the creative return: pickup %v, removal %v, want both", picked, removed)
	}
	if count, _ := tridentsHeld(srv, "Maker"); count != 1 {
		t.Errorf("the creative owner holds %d tridents after the loyal return, want the one it never spent", count)
	}
}

// TestARiptideTridentIsNeverThrown gives a trident carrying riptide and
// charges it: the draw never starts, as vanilla refuses a riptide draw
// outside water or rain, so the release throws nothing and the stack stays in hand.
func TestARiptideTridentIsNeverThrown(t *testing.T) {
	t.Parallel()
	srv, c, _ := servertest.JoinAs(t, servertest.BowTestConfig(), "Thrower")
	giveRiptideTrident(srv, "Thrower", 1)

	c.MoveTo(0.5, 64, 0.5, -90, 0)
	c.Settle()
	srv.StopTick()
	servertest.DrawBow(c, -90, 0)
	c.Settle()
	for range 15 {
		srv.TickSurvival()
	}
	if ticks := srv.BowTicks("Thrower"); ticks != 0 {
		t.Errorf("a riptide trident charged for %d ticks, want no charge at all", ticks)
	}
	servertest.ReleaseBow(c)
	c.Settle()
	servertest.HoldTicks(t, srv)

	for gotID, data := range tridentPackets(c, srv, 40) {
		if gotID != protocol.IDPlaySpawnEntity {
			continue
		}
		r := protocol.NewReader(data)
		r.ReadVarInt()
		r.ReadUUID()
		if got := r.ReadVarInt(); got == protocol.EntityTypeTrident {
			t.Fatalf("a riptide trident was thrown")
		}
	}
	if count, _ := tridentsHeld(srv, "Thrower"); count != 1 {
		t.Errorf("the riptide thrower holds %d tridents after the release, want the one unthrown", count)
	}
}

func TestChannelingTridentCallsLightning(t *testing.T) {
	for _, tc := range []struct {
		name                                            string
		thunder, enchanted, roof, silent, refused, want bool
	}{
		{name: "open sky", thunder: true, enchanted: true, want: true},
		{name: "clear", enchanted: true},
		{name: "roof", thunder: true, enchanted: true, roof: true},
		{name: "plain", thunder: true},
		{name: "refused", thunder: true, enchanted: true, refused: true},
		{name: "silent", thunder: true, enchanted: true, silent: true, want: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			srv, c, _ := servertest.JoinAs(t, servertest.BowTestConfig(), "Thrower", servertest.OpsListing(t, "Thrower"))
			servertest.HoldTicks(t, srv)
			prepareChanneling(t, srv, c, tc.thunder, tc.enchanted)
			cowID := srv.SpawnMob("minecraft:cow", 10.5, 64, 0.5)
			c.ExpectMobSpawn(protocol.EntityTypeCow)
			// The victim's fresh hurt window may refuse lightning; a nearby
			// unhurt cow proves the bolt is damaging rather than visual-only.
			bystanderID := srv.SpawnMob("minecraft:cow", 12.5, 64, 0.5)
			c.ExpectMobSpawn(protocol.EntityTypeCow)
			if tc.refused {
				srv.SetMobInvulnerable(cowID)
			}
			if tc.silent {
				srv.SetMobSilent(cowID, true)
			}
			if tc.roof {
				for x := int32(1); x <= 7; x++ {
					for z := int32(-3); z <= 3; z++ {
						srv.SetBlock(x, 68, z, registry.BlockStone.DefaultState())
					}
				}
				srv.OverworldWorld().Relight(0, -1)
				srv.OverworldWorld().Relight(0, 0)
				srv.OverworldWorld().FlushLighting()
			}
			c.MoveTo(0.5, 64, 0.5, -90, 12)
			startTridentCharge(t, srv, c, "Thrower", -90, 12, 12)
			srv.MoveMob(cowID, 4.5, 0.5)
			srv.MoveMob(bystanderID, 6.5, 0.5)
			endTridentCharge(t, srv, c, "Thrower")
			expectTridentSpawn(c, srv)
			bolt, sound, hurt, lightningHurt := false, false, false, false
			for id, data := range tridentPackets(c, srv, 20) {
				if id == protocol.IDPlayDamageEvent {
					r := protocol.NewReader(data)
					victim, damageType := r.ReadVarInt(), r.ReadVarInt()
					hurt = hurt || (victim == cowID && damageType == registry.MustID("minecraft:damage_type", rules.DamageTypeTrident.Name()))
					lightningHurt = lightningHurt || (victim == bystanderID && damageType == registry.MustID("minecraft:damage_type", rules.DamageTypeLightningBolt.Name()))
				}
				if x, y, z, ok := channelBolt(data, id); ok {
					bolt = true
					if rules.BlockOf(x) != 4 || rules.BlockOf(y) != 64 || rules.BlockOf(z) != 0 {
						t.Errorf("bolt at (%g,%g,%g), want cow's block (4,64,0)", x, y, z)
					}
				}
				if id == protocol.IDPlaySoundEffect {
					sound = channelThunder(c, data, 4, 64, 0) || sound
				}
			}
			if hurt == tc.refused {
				t.Fatalf("hurt=%v, want %v", hurt, !tc.refused)
			}
			if lightningHurt != tc.want {
				t.Errorf("lightning hurt=%v, want %v", lightningHurt, tc.want)
			}
			if bolt != tc.want || sound != (tc.want && !tc.silent) {
				t.Errorf("bolt=%v sound=%v, want %v %v", bolt, sound, tc.want, tc.want && !tc.silent)
			}
		})
	}
}

func TestChannelingTridentHitsBlocks(t *testing.T) {
	for _, tc := range []struct {
		name                           string
		block                          registry.Block
		roof, thunder, enchanted, want bool
	}{
		{name: "rod", block: registry.BlockLightningRod, thunder: true, enchanted: true, want: true},
		{name: "stone", block: registry.BlockStone, thunder: true, enchanted: true},
		{name: "roofed rod", block: registry.BlockLightningRod, roof: true, thunder: true, enchanted: true},
		{name: "clear rod", block: registry.BlockLightningRod, enchanted: true},
		{name: "plain rod", block: registry.BlockLightningRod, thunder: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			srv, c, _ := servertest.JoinAs(t, servertest.BowTestConfig(), "Thrower", servertest.OpsListing(t, "Thrower"))
			servertest.HoldTicks(t, srv)
			prepareChanneling(t, srv, c, tc.thunder, tc.enchanted)
			srv.SetBlock(4, 65, 0, tc.block.DefaultState())
			if tc.roof {
				for x := int32(1); x <= 7; x++ {
					for z := int32(-3); z <= 3; z++ {
						srv.SetBlock(x, 68, z, registry.BlockStone.DefaultState())
					}
				}
			}
			srv.OverworldWorld().Relight(0, -1)
			srv.OverworldWorld().Relight(0, 0)
			srv.OverworldWorld().FlushLighting()
			if tc.roof && srv.SkyLightAt(4, 65, 0) == 15 {
				t.Fatal("roof leaves rod exposed to sky")
			}
			c.MoveTo(0.5, 64, 0.5, -90, 0)
			startTridentCharge(t, srv, c, "Thrower", -90, 0, 12)
			endTridentCharge(t, srv, c, "Thrower")
			thrown := expectTridentSpawn(c, srv)
			bolt, sound, stuck := false, false, false
			for id, data := range tridentPackets(c, srv, 20) {
				if id == protocol.IDPlayEntityMetadata && protocol.NewReader(data).ReadVarInt() == thrown.entityID {
					stuck = stuck || readTridentMetadata(c, data).inGround
				}
				if x, y, z, ok := channelBolt(data, id); ok {
					bolt = true
					if rules.BlockOf(x) != 4 || rules.BlockOf(y) != 65 || rules.BlockOf(z) != 0 {
						t.Errorf("bolt at (%g,%g,%g), want hit block (4,65,0)", x, y, z)
					}
				}
				if id == protocol.IDPlaySoundEffect {
					sound = channelThunder(c, data, 4, 65, 0) || sound
				}
			}
			if !stuck {
				t.Fatal("trident missed the block")
			}
			if bolt != tc.want || sound != tc.want {
				t.Errorf("bolt=%v sound=%v, want %v", bolt, sound, tc.want)
			}
		})
	}
}

func prepareChanneling(t *testing.T, srv *server.Server, c *servertest.Client, thunder, enchanted bool) {
	t.Helper()
	// Keep random weather strikes out of the projectile assertions.
	srv.SetLightningOdds(int(^uint(0) >> 1))
	if thunder {
		c.ChatCommand("weather thunder 6000")
		c.ExpectCommandFeedback("commands.weather.set.thunder")
		for range 400 {
			srv.TickWeather()
		}
		if !srv.Thundering() {
			t.Fatal("weather command did not produce thunder")
		}
	}
	c.Settle()
	stack := protocol.ItemStack{ItemID: servertest.MustItem("minecraft:trident"), Count: 1}
	if enchanted {
		id, _ := registry.EnchantmentID("minecraft:channeling")
		stack.Enchantments = map[int32]int32{id: 1}
	}
	srv.GiveSlot("Thrower", 36, stack)
}

func channelBolt(data []byte, id int32) (x, y, z float64, ok bool) {
	if id != protocol.IDPlaySpawnEntity {
		return
	}
	r := protocol.NewReader(data)
	r.ReadVarInt()
	r.ReadUUID()
	boltType, _ := registry.EntityTypeID("minecraft:lightning_bolt")
	if r.ReadVarInt() != boltType {
		return
	}
	return r.ReadF64(), r.ReadF64(), r.ReadF64(), true
}

// channelThunder checks the sound's position in eighths of a block.
func channelThunder(c *servertest.Client, data []byte, bx, by, bz int32) bool {
	c.T.Helper()
	name, _, x, y, z, volume, pitch := servertest.ReadAmbient(c.T, data)
	if name != "minecraft:item.trident.thunder" {
		return false
	}
	if volume != 5 || pitch != 1 {
		c.T.Errorf("thunder volume=%g pitch=%g, want 5 and 1", volume, pitch)
	}
	if rules.BlockOf(float64(x)/8) != bx || rules.BlockOf(float64(y)/8) != by || rules.BlockOf(float64(z)/8) != bz {
		c.T.Errorf("thunder at (%g,%g,%g), want block (%d,%d,%d)", float64(x)/8, float64(y)/8, float64(z)/8, bx, by, bz)
	}
	return true
}
