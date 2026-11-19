package explosions

import (
	"testing"

	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/internal/server/servertest"
	"github.com/golem-mc/golem/internal/server/world"
	"github.com/golem-mc/golem/registry"
	"github.com/google/uuid"
)

// The primed TNT tests: the lit block a flint and steel leaves standing in
// the world, driven the way the creeper's blast is — through a real
// connection, in the flat world the harness grows. A summoned body shows the
// fuse its metadata carries, falls the fall its gravity gives it and goes up
// at the end of the fuse; a survival player near it takes the blast through
// the pipeline every other hurt travels; a TNT block under a flint and steel
// primes instead of lighting fire; and a TNT block a blast reaches primes
// instead of dropping.

// tntSkip is the background a watching client hears while a primed TNT is
// being watched: the world's own traffic and the sounds it makes. The block
// updates are kept — the crater is read out of them — and so are the
// movement and metadata packets, the fall and the fuse riding them.
func tntSkip(id int32) bool {
	switch id {
	case protocol.IDPlayUpdateTime,
		protocol.IDPlayUpdateLight, protocol.IDPlayChunkData,
		protocol.IDPlayChunkBatchStart, protocol.IDPlayChunkBatchFinished,
		protocol.IDPlayCenterChunk, protocol.IDPlayUnloadChunk,
		protocol.IDPlayWorldEvent, protocol.IDPlaySoundEffect,
		protocol.IDPlayEntityAnimation, protocol.IDPlayEntityEquipment,
		protocol.IDPlayContainerSlot, protocol.IDPlayPingResponse,
		protocol.IDPlaySetEntityMotion:
		return true
	}
	return false
}

// TestABlastTellsItsCraterBatchedPerSection checks what a watching client is
// told a crater in: one Update Section Blocks packet per chunk section the
// blast touched — every removed cell landing on the client at once — and
// one Explosion packet for the blast, not one Block Update per cell
// arriving one after another. The crater's own width is what the ground
// says it lost.
func TestABlastTellsItsCraterBatchedPerSection(t *testing.T) {
	t.Parallel()
	srv, c := servertest.Join(t, servertest.BlockTestConfig(), "Watcher")

	// The stone before the blast, read over a box wide enough to hold
	// every cell the TNT's rays chew.
	snapshot := stoneCellsAround(srv.BlockAt, 0, 9, 54, 70, -4, 5)

	// A summoned body on a one-tick fuse: the spawn goes by, the crater and
	// the blast's own packet follow.
	if srv.SpawnTNT(4.5, 64, 0.5, 1) == 0 {
		t.Fatal("the primed body never stood up")
	}
	blockUpdates := 0
	sectionPackets := 0
	sectionEntries := 0
	ok := c.ReadTicksSkipping(srv, 400, tntSkip, func(id int32, data []byte) bool {
		switch id {
		case protocol.IDPlayBlockUpdate:
			r := protocol.NewReader(data)
			r.ReadPosition()
			if uint16(r.ReadVarInt()) == 0 {
				blockUpdates++
			}
		case protocol.IDPlaySectionBlocks:
			_, entries := servertest.ReadSectionBlocks(c.T, data)
			sectionPackets++
			for _, entry := range entries {
				if uint16(entry>>12) == 0 {
					sectionEntries++
				}
			}
		case protocol.IDPlayExplosion:
			boom := servertest.ReadExplosion(c, data)
			if boom.Radius != 4.0 {
				c.T.Errorf("servertest.Explosion radius = %v, want the primed TNT's 4", boom.Radius)
			}
			if boom.BlockCount <= 0 {
				c.T.Errorf("servertest.Explosion blockCount = %d, want the ray-cast's own count", boom.BlockCount)
			}
			// The crater updates were sent before the Explosion packet,
			// so ending the wait here has counted them all.
			return true
		}
		return false
	})
	if !ok {
		t.Fatal("the fuse never ran out")
	}

	destroyed := snapshot.destroyedNow(srv.BlockAt)
	if destroyed < 5 {
		t.Fatalf("the blast took %d stones, want a crater worth batching", destroyed)
	}
	if told := blockUpdates + sectionEntries; told != destroyed {
		t.Errorf("the blast told the client of %d of its %d removed cells, want every cell exactly once",
			told, destroyed)
	}
	if sectionPackets == 0 {
		t.Errorf("the blast told its %d removed cells one Block Update at a time and no section update, want the crater batched per section",
			destroyed)
	}
	if packets := blockUpdates + sectionPackets; packets >= destroyed {
		t.Errorf("the blast told its %d removed cells in %d packets, want them batched into fewer",
			destroyed, packets)
	}
}

// tntSpawn is one primed TNT's Spawn Entity packet read whole: the body's
// entity ID, the entity type it spawned as, where it stood and how fast the
// nudge or the stillness of a summon left it moving.
type tntSpawn struct {
	entityID   int32
	typeID     int32
	x, y, z    float64
	vx, vy, vz float64
}

// spawnEntityHead reads the head of a Spawn Entity packet — the entity ID,
// the UUID it spawned under and the entity type — and gives the rest back
// unread, so a caller can tell whether the arrival is one of its own before
// parsing it whole.
func spawnEntityHead(data []byte) (int32, int32) {
	r := protocol.NewReader(data)
	id := r.ReadVarInt()
	if r.Err() != nil {
		return 0, 0
	}
	r.ReadUUID()
	if r.Err() != nil {
		return 0, 0
	}
	return id, r.ReadVarInt()
}

// readTNTSpawn parses a Spawn Entity packet that has already been found to
// carry a primed TNT, checking it against this repository's own field
// layouts, leftover bytes and all.
func readTNTSpawn(c *servertest.Client, data []byte) tntSpawn {
	c.T.Helper()
	r := protocol.NewReader(data)
	s := tntSpawn{}
	s.entityID = r.ReadVarInt()
	if s.entityID == 0 {
		c.T.Fatal("tnt spawn ID = 0, want a real entity ID")
	}
	if r.ReadUUID() == (uuid.UUID{}) {
		c.T.Error("tnt spawn UUID = nil, want the one it was spawned as")
	}
	s.typeID = r.ReadVarInt()
	if s.typeID != protocol.EntityTypeTnt {
		c.T.Errorf("spawned entity type = %d, want minecraft:tnt's %d", s.typeID, protocol.EntityTypeTnt)
	}
	s.x, s.y, s.z = r.ReadF64(), r.ReadF64(), r.ReadF64()
	s.vx, s.vy, s.vz = r.ReadLpVec3()
	r.ReadAngle() // pitch, which a primed TNT does not turn
	r.ReadAngle() // yaw
	r.ReadAngle() // head yaw
	if got := r.ReadVarInt(); got != 0 {
		c.T.Errorf("tnt spawn carries type data %d, want none", got)
	}
	if err := r.Err(); err != nil {
		c.T.Fatalf("spawn entity (tnt): %v", err)
	}
	if r.Remaining() != 0 {
		c.T.Errorf("spawn entity (tnt) has %d bytes left over", r.Remaining())
	}
	return s
}

// readFuseUpdate parses a Set Entity Metadata packet that carries a primed
// TNT's fuse alone, the one field the burn moves, and reports the body it
// was about and the fuse it said. A packet about some other body — an item
// entity's stack among them — is reported as none of this body's, the
// caller reading past it.
func readFuseUpdate(c *servertest.Client, data []byte, entityID int32) (int32, bool) {
	c.T.Helper()
	r := protocol.NewReader(data)
	if got := r.ReadVarInt(); got != entityID {
		return -1, false
	}
	fuse := int32(-1)
	for r.Err() == nil {
		index := r.ReadU8()
		if index == protocol.MetadataEnd {
			break
		}
		fieldType := r.ReadVarInt()
		switch {
		case index == protocol.MetadataIndexTNTFuse && fieldType == protocol.MetadataVarInt:
			fuse = r.ReadVarInt()
		default:
			c.T.Fatalf("unexpected fuse-update field %d of type %d", index, fieldType)
		}
	}
	if err := r.Err(); err != nil {
		c.T.Fatalf("fuse update: %v", err)
	}
	if r.Remaining() != 0 {
		c.T.Errorf("fuse update has %d bytes left over", r.Remaining())
	}
	return fuse, true
}

// TestASummonedTNTFallsAndExplodesAtItsFuse checks the whole of a summoned
// body's life over the wire: the spawn naming minecraft:tnt where it was
// summoned, still; the metadata carrying the fuse it was lit with; the fall
// the gravity gives it, told in movement packets until the ground takes it;
// the fuse ticking down in its metadata; and the end — the body removed, an
// Explosion packet with the vanilla radius four standing where the body
// stood, and the surface around it told as air and read back as it.
func TestASummonedTNTFallsAndExplodesAtItsFuse(t *testing.T) {
	t.Parallel()
	cfg := servertest.BlockTestConfig() // creative, so the blast's drops stay behind

	srv, c := servertest.Join(t, cfg, "Watcher")

	tntID := srv.SpawnTNT(4.5, 70, 0.5, 40)
	if tntID == 0 {
		t.Fatal("SpawnTNT came to nothing")
	}

	spawned := false
	var body tntSpawn
	spawnFuse, spawnFuseSeen := int32(0), false
	firstMeta := true
	landed := false
	landY := 0.0
	fuseSeen := int32(0)
	exploded := false
	var boom servertest.Explosion
	removed := false
	airUpdates := 0
	surfaceGone := false
	c.ReadTicksSkipping(srv, 400, tntSkip, func(id int32, data []byte) bool {
		switch id {
		case protocol.IDPlaySpawnEntity:
			if gotID, gotType := spawnEntityHead(data); gotID != tntID || gotType != protocol.EntityTypeTnt {
				return false // another body's arrival
			}
			body = readTNTSpawn(c, data)
			spawned = true
		case protocol.IDPlayEntityMetadata:
			fuse, ours := readFuseUpdate(c, data, tntID)
			if !ours {
				return false // another body's state, read past
			}
			if firstMeta {
				// The metadata the spawn carried: the fuse the body was
				// lit with, whatever that was.
				spawnFuse = fuse
				spawnFuseSeen = true
				firstMeta = false
				return false
			}
			if fuse >= 0 && fuse < 40 {
				fuseSeen = fuse
			}
		case protocol.IDPlayTeleportEntity:
			r := protocol.NewReader(data)
			if got := r.ReadVarInt(); got != tntID {
				return false
			}
			r.ReadVarInt() // position path
			x, y, z := r.ReadF64(), r.ReadF64(), r.ReadF64()
			r.ReadF32() // yaw
			r.ReadF32() // pitch
			if r.ReadBool() {
				landed = true
				landY = y
				if x != 4.5 || z != 0.5 {
					c.T.Errorf("the body landed at (%v %v %v), want the column it was summoned in", x, y, z)
				}
			}
		case protocol.IDPlayBlockUpdate:
			r := protocol.NewReader(data)
			x, y, z := r.ReadPosition()
			if uint16(r.ReadVarInt()) == 0 {
				airUpdates++
				if x == 4 && y == 63 && z == 0 {
					surfaceGone = true
				}
			}
		case protocol.IDPlaySectionBlocks:
			// The blast's crater arrives batched per section; the entries
			// carry each removed cell's state and section-local place.
			pos, entries := servertest.ReadSectionBlocks(c.T, data)
			for _, entry := range entries {
				if uint16(entry>>12) != 0 {
					continue
				}
				airUpdates++
				x := int32(pos>>42)<<4 | int32((entry>>8)&0xF)
				y := int32((pos<<44)>>44)<<4 | int32(entry&0xF)
				z := int32((pos<<22)>>42)<<4 | int32((entry>>4)&0xF)
				if x == 4 && y == 63 && z == 0 {
					surfaceGone = true
				}
			}
		case protocol.IDPlayExplosion:
			boom = servertest.ReadExplosion(c, data)
			if boom.Radius != 4.0 {
				c.T.Errorf("servertest.Explosion radius = %v, want the primed TNT's 4", boom.Radius)
			}
			exploded = true
		case protocol.IDPlayRemoveEntities:
			r := protocol.NewReader(data)
			if got := r.ReadVarInt(); got != 1 {
				return false // another body's removal
			}
			if got := r.ReadVarInt(); got != tntID {
				c.T.Errorf("removed entity = %d, want the primed TNT's %d", got, tntID)
			}
			removed = true
		}
		return spawned && landed && exploded && removed &&
			spawnFuseSeen && fuseSeen > 0 && fuseSeen < 40 && surfaceGone
	})
	if !spawned {
		t.Fatal("the summoned body's spawn never arrived")
	}
	if body.x != 4.5 || body.y != 70 || body.z != 0.5 {
		t.Errorf("the body spawned at (%v %v %v), want where the summon put it", body.x, body.y, body.z)
	}
	if body.vx != 0 || body.vy != 0 || body.vz != 0 {
		t.Errorf("the summoned body moved at (%v %v %v), want the stillness of a summon", body.vx, body.vy, body.vz)
	}
	if !spawnFuseSeen {
		t.Fatal("the body's spawn never carried its fuse in the metadata")
	}
	if spawnFuse != 40 {
		t.Errorf("the spawn's metadata fuse = %d, want the 40 the body was lit with", spawnFuse)
	}
	if !landed {
		t.Error("the body was never told landing by an on-ground teleport")
	}
	if landY != 64 {
		t.Errorf("the body landed at %v, want the flat world's surface at 64", landY)
	}
	if fuseSeen == 0 {
		t.Error("the fuse never ticked down in the metadata")
	}
	if !exploded {
		t.Fatal("the fuse ran out without an Explosion packet")
	}
	if boom.X != 4.5 || boom.Z != 0.5 {
		t.Errorf("the blast stood at (%v %v %v), want the body's own column", boom.X, boom.Y, boom.Z)
	}
	if boom.Y < 64 || boom.Y > 64.2 {
		t.Errorf("the blast stood at y=%v, want the body's height share above its feet", boom.Y)
	}
	if !removed {
		t.Error("the exploding body was never taken out of the world")
	}
	if airUpdates == 0 {
		t.Error("the blast never told the client of air")
	}
	if !surfaceGone {
		if srv.BlockAt(4, 63, 0) == 0 {
			t.Error("the blast took the surface under the body without ever telling the air of it")
		} else {
			t.Error("the blast left the surface under the body standing")
		}
	}
}

// TestASurvivalPlayerNearTNTIsHurt checks the blast's share of a survival
// body standing near it: the damage event naming the unowned blast's own
// type, the direct carrier the primed body was, and the health the blow
// left.
func TestASurvivalPlayerNearTNTIsHurt(t *testing.T) {
	t.Parallel()
	cfg := servertest.HostileTestConfig() // survival, the mode that can be hurt

	srv, c, playerID := servertest.JoinAs(t, cfg, "Bystander")

	// The body four blocks out, on the ground: the blast reaches the double
	// radius of eight, and the flat world's own surface is the food it
	// chews first.
	tntID := srv.SpawnTNT(4.5, 64, 0.5, 20)

	hurt := false
	damageEvent := false
	exploded := false
	c.ReadTicksSkipping(srv, 400, tntSkip, func(id int32, data []byte) bool {
		switch id {
		case protocol.IDPlayExplosion:
			exploded = true
		case protocol.IDPlaySetHealth:
			h := servertest.ExpectHealthReport(c, data)
			if h.Health < 20 && !hurt {
				hurt = true
			}
		case protocol.IDPlayDamageEvent:
			r := protocol.NewReader(data)
			victim := r.ReadVarInt()
			if victim != playerID {
				return false
			}
			if got, want := r.ReadVarInt(), registry.MustID("minecraft:damage_type", "minecraft:explosion"); got != want {
				c.T.Errorf("damage type ID = %d, want the unowned blast's %d", got, want)
			}
			if got := r.ReadVarInt(); got != 0 {
				c.T.Errorf("damage cause ID = %d, want nobody, the blast having no owner", got)
			}
			if got := r.ReadVarInt(); got != tntID+1 {
				c.T.Errorf("damage direct ID = %d, want the primed body's %d", got, tntID+1)
			}
			damageEvent = true
		}
		return hurt && damageEvent && exploded
	})
	if !exploded {
		t.Fatal("the body never went up")
	}
	if !hurt {
		t.Error("the blast never hurt the survival player beside it")
	}
	if !damageEvent {
		t.Error("the blow never came with its damage event")
	}
}

// TestFlintAndSteelPrimesATNTBlock checks the lit block: a TNT block struck
// with a flint and steel answers the click the way TntBlock.useItemOn does —
// the block taken out, a primed body stood up where it stood moving with the
// nudge its constructor gave it, and the steel worn a wear for the lighting
// in survival.
func TestFlintAndSteelPrimesATNTBlock(t *testing.T) {
	t.Parallel()
	cfg := servertest.HostileTestConfig() // survival, the mode that pays wear

	srv, c, _ := servertest.JoinAs(t, cfg, "Lighter")

	tnt, ok := registry.FindState("minecraft:tnt", nil)
	if !ok {
		t.Fatal("the block registry has no minecraft:tnt")
	}
	srv.SetBlock(2, 64, 0, tnt)
	flintID, ok := registry.ItemID("minecraft:flint_and_steel")
	if !ok {
		t.Fatal("the item registry has no minecraft:flint_and_steel")
	}
	srv.GiveSlot("Lighter", 36, protocol.ItemStack{ItemID: flintID, Count: 1})

	// The steel is struck on the block itself: the top face of the TNT,
	// which is where the click lands.
	c.PlaceBlock(2, 64, 0, protocol.FaceTop)

	var body tntSpawn
	found := false
	acked := false
	c.ReadTicksSkipping(srv, 100, tntSkip, func(id int32, data []byte) bool {
		switch id {
		case protocol.IDPlaySpawnEntity:
			if _, gotType := spawnEntityHead(data); gotType != protocol.EntityTypeTnt {
				return false // another body's arrival
			}
			body = readTNTSpawn(c, data)
			found = true
		case protocol.IDPlayAckBlockChange:
			acked = true
		}
		return found && acked
	})
	if !found {
		t.Fatal("the struck block never stood a primed body up")
	}
	if !acked {
		t.Error("the priming was never acknowledged")
	}
	if got := srv.BlockAt(2, 64, 0); got != 0 {
		t.Errorf("the struck block = %d, want air where it stood", got)
	}
	if body.x != 2.5 || body.y != 64 || body.z != 0.5 {
		t.Errorf("the body stood at (%v %v %v), want the block's centre bottom", body.x, body.y, body.z)
	}
	if body.vy < 0.15 || body.vy > 0.25 {
		t.Errorf("the body's upward push = %v, want the constructor's fifth of a block", body.vy)
	}
	if body.vx < -0.03 || body.vx > 0.03 || body.vz < -0.03 || body.vz > 0.03 {
		t.Errorf("the body's sideways push = (%v %v), want the constructor's two hundredths a side", body.vx, body.vz)
	}
	// The wear lands on the steel behind the spawn and the ack, so it is
	// waited for rather than read at once.
	servertest.WaitFor(t, srv, 100, func() bool { return srv.InventorySlot("Lighter", 36).Damage != 0 })
	if got := srv.InventorySlot("Lighter", 36); got.Damage != 1 {
		t.Errorf("the steel's wear after the lighting = %d, want the vanilla one", got.Damage)
	}
}

// TestATNTBlockCaughtInABlastPrimesInsteadOfDropping checks the chain: a TNT
// block a blast reaches is destroyed the way any block is — and then, the
// way TntBlock.wasExploded answers, stands a primed body up where it was
// instead of leaving the block's own drops behind.
func TestATNTBlockCaughtInABlastPrimesInsteadOfDropping(t *testing.T) {
	t.Parallel()
	cfg := servertest.BlockTestConfig() // creative, so nothing else is keeping drops

	srv, c := servertest.Join(t, cfg, "Chainer")

	tnt, ok := registry.FindState("minecraft:tnt", nil)
	if !ok {
		t.Fatal("the block registry has no minecraft:tnt")
	}
	// The block three blocks east of where the first body stands: inside
	// the double radius, and inside the reach of the rays the blast fires
	// level with it.
	srv.SetBlock(3, 64, 0, tnt)
	fuseID, ok := registry.ItemID("minecraft:tnt")
	if !ok {
		t.Fatal("the item registry has no minecraft:tnt")
	}
	srv.SpawnTNT(0.5, 64, 0.5, 20)

	exploded := false
	var chain tntSpawn
	chainFound := false
	dropped := false
	c.ReadTicksSkipping(srv, 500, tntSkip, func(id int32, data []byte) bool {
		switch id {
		case protocol.IDPlayExplosion:
			exploded = true
		case protocol.IDPlaySpawnEntity:
			if _, gotType := spawnEntityHead(data); gotType != protocol.EntityTypeTnt {
				return false // an item entity's arrival, or another body's
			}
			spawn := readTNTSpawn(c, data)
			if spawn.x != 3.5 {
				return false // the body the summon stood up, already counted
			}
			chain = spawn
			chainFound = true
		case protocol.IDPlayEntityMetadata:
			// An item entity's arrival carries its stack: a tnt item
			// lying where the block stood is the drop the chain's rules
			// refuse.
			r := protocol.NewReader(data)
			r.ReadVarInt() // the entity, which the stack's own arrival names
			for r.Err() == nil {
				index := r.ReadU8()
				if index == protocol.MetadataEnd {
					break
				}
				fieldType := r.ReadVarInt()
				if index == protocol.MetadataIndexItemSlot && fieldType == protocol.MetadataSlot {
					stack, _ := r.ReadSlot()
					if !stack.Empty() && stack.ItemID == fuseID {
						dropped = true
					}
					continue
				}
				stepOverMetadataValue(r, fieldType)
			}
		}
		return exploded && chainFound && srv.BlockAt(3, 64, 0) == 0
	})
	if !exploded {
		t.Fatal("the first body never went up")
	}
	if got := srv.BlockAt(3, 64, 0); got != 0 {
		t.Fatalf("the caught block = %d, want air where the blast took it", got)
	}
	if !chainFound {
		t.Fatal("the caught block never stood a primed body of its own up")
	}
	if chain.y != 64 || chain.z != 0.5 {
		t.Errorf("the chain body stood at (%v %v %v), want the block's centre bottom", chain.x, chain.y, chain.z)
	}
	if chain.vy < 0.15 || chain.vy > 0.25 {
		t.Errorf("the chain body's upward push = %v, want the constructor's fifth of a block", chain.vy)
	}
	if got := srv.LoadBlockAt(3, 64, 0); got != 0 {
		t.Errorf("the caught block on disk = %d, want air where the blast took it", got)
	}
	if dropped {
		t.Error("the caught block left its own item behind, the drop TntBlock.dropFromExplosion refuses")
	}
}

// TestATNTBlastDestroysTheItemsItReaches checks the blast's reach over the
// item entities lying in it: a dirt stack a block from the body's blast and
// a netherite ingot two blocks out are both destroyed by it — removed from
// the client's view and gone from the world, with no fresh body carrying the
// stack standing where the blast caught it — while the damage-resistant
// nether star a block out survives the same blast, its damage_resistant
// naming the explosion tags where netherite's names only fire's.
func TestATNTBlastDestroysTheItemsItReaches(t *testing.T) {
	t.Parallel()
	cfg := servertest.BlockTestConfig() // creative, so nothing else is keeping drops

	srv, c := servertest.Join(t, cfg, "Watcher")

	for _, name := range []string{"minecraft:dirt", "minecraft:nether_star", "minecraft:netherite_ingot"} {
		if _, ok := registry.ItemID(name); !ok {
			t.Fatalf("the item registry has no %s", name)
		}
	}

	// A dirt stack a block west of where the body will stand, a netherite
	// ingot two blocks south and a nether star a block east, all lying on
	// the surface inside the double radius: only the stacks'
	// damage_resistant can part the star from the rest.
	dirtID := srv.SpawnItemEntity(3.5, 64.125, 0.5, "minecraft:dirt", 1)
	ingotID := srv.SpawnItemEntity(4.5, 64.125, 2.5, "minecraft:netherite_ingot", 1)
	starID := srv.SpawnItemEntity(5.5, 64.125, 0.5, "minecraft:nether_star", 1)
	if dirtID == 0 || ingotID == 0 || starID == 0 {
		t.Fatal("SpawnItemEntity came to nothing")
	}
	if tntID := srv.SpawnTNT(4.5, 64, 0.5, 40); tntID == 0 {
		t.Fatal("SpawnTNT came to nothing")
	}

	exploded := false
	removed := map[int32]bool{}
	c.ReadTicksSkipping(srv, 500, tntSkip, func(id int32, data []byte) bool {
		switch id {
		case protocol.IDPlayExplosion:
			exploded = true
		case protocol.IDPlayRemoveEntities:
			r := protocol.NewReader(data)
			for n := r.ReadVarInt(); n > 0 && r.Err() == nil; n-- {
				removed[r.ReadVarInt()] = true
			}
		}
		return exploded && removed[dirtID] && removed[ingotID]
	})
	if !exploded {
		t.Fatal("the body never went up")
	}
	if !removed[dirtID] {
		t.Error("the dirt was never removed, the blast's reach passing it by")
	}
	if !removed[ingotID] {
		t.Error("the netherite ingot was never removed, its damage_resistant naming only the fire tags")
	}
	if removed[starID] {
		t.Error("the nether star was removed, its damage_resistant putting it past the blast")
	}

	// The world itself, once the item pass has swept the wreck up: the
	// dirt and the ingot gone for good — no fresh body carrying either
	// standing where the blast caught them — and the star still lying
	// there, its own body. Nothing of the destroyed two comes back over
	// the wire meanwhile, the wreck's packets going nowhere.
	c.PumpTicks(srv, 10, func(id int32, data []byte) bool {
		switch id {
		case protocol.IDPlaySpawnEntity, protocol.IDPlayTeleportEntity,
			protocol.IDPlayEntityMetadata:
			r := protocol.NewReader(data)
			if got := r.ReadVarInt(); got == dirtID || got == ingotID {
				t.Errorf("packet %d came for destroyed body %d after its removal", id, got)
			}
		}
		return false
	})
	for _, it := range srv.ItemsInDimension(world.DimensionName) {
		switch it.Item {
		case "minecraft:dirt":
			t.Errorf("a dirt stack is still in the world: entity %d at (%v, %v, %v) with %d, the destroyed one come again",
				it.ID, it.X, it.Y, it.Z, it.Count)
		case "minecraft:netherite_ingot":
			t.Errorf("a netherite ingot is still in the world: entity %d at (%v, %v, %v) with %d, the destroyed one come again",
				it.ID, it.X, it.Y, it.Z, it.Count)
		case "minecraft:nether_star":
			if it.ID != starID {
				t.Errorf("the star stands again as entity %d, want the body it was laid as (%d)", it.ID, starID)
			}
		}
	}
}

// stepOverMetadataValue steps over one metadata value of the type named, the
// ones an item entity's arrival carries, so a metadata walk can reach the
// next field without knowing what each one means.
func stepOverMetadataValue(r *protocol.Reader, fieldType int32) {
	switch fieldType {
	case protocol.MetadataByte, protocol.MetadataBoolean:
		r.ReadU8()
	case protocol.MetadataVarInt, protocol.MetadataHumanoidArm:
		r.ReadVarInt()
	case protocol.MetadataFloat:
		r.ReadF32()
	case protocol.MetadataSlot:
		r.ReadSlot()
	default:
		// Nothing else an item entity carries; the walk stops rather
		// than guessing a length.
		r.Rest()
	}
}
