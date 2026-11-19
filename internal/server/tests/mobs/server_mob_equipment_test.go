package mobs

import (
	"github.com/golem-mc/golem/internal/server/servertest"

	"fmt"
	"math"
	"testing"

	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/internal/server/player"
	"github.com/golem-mc/golem/internal/server/rules"
	"github.com/golem-mc/golem/internal/server/world"
	"github.com/golem-mc/golem/nbt"
	"github.com/golem-mc/golem/registry"
)

// The mob equipment tests: what a spawn stands its zombies up wearing —
// Mob.populateDefaultEquipmentSlots' armour roll by the difficulty and
// Zombie's own iron sword, spear or shovel — the wire it arrives shown by,
// the blow a chestplate turns, and the sun a helmet keeps off a zombie's
// head. Where a test needs a body wearing a known piece it summons one
// through the equipment tag; the armour the spawn rolled itself is read off
// the bodies it plants and the packets they arrive with.

// fullMoonMidnight is the clock that both keeps a spawn's difficulty at its
// largest and the sun out of the world: day sixty-four of the world's clock
// — a full moon, whose brightness shares its whole quarter out — at its
// midnight, the hours no zombie burns in.
const fullMoonMidnight = 64*24000 + 18000

// spawnGroup reads one mob's spawn grouping as the wire carries it — the
// body, its state, whatever it arrives holding and wearing, and its pace —
// answering the entity ID and the Set Equipment entries, nil for a bare
// spawn the pairing named no equipment packet for.
func spawnGroup(c *servertest.Client, wantType int32) (int32, []servertest.EquipmentEntry) {
	c.T.Helper()
	spawn := protocol.NewReader(c.ExpectMobPacket(protocol.IDPlaySpawnEntity))
	id := spawn.ReadVarInt()
	spawn.ReadUUID()
	if got := spawn.ReadVarInt(); got != wantType {
		c.T.Errorf("spawned entity type = %d, want %d", got, wantType)
	}
	if err := spawn.Err(); err != nil {
		c.T.Fatalf("spawn entity: %v", err)
	}
	c.ExpectMobPacket(protocol.IDPlayEntityMetadata)

	// The equipment a body arrives with stands between its state and its
	// pace, and only a body with something on arrives with one; the pace
	// packet ends the grouping. Whatever else a running loop tells about
	// any body between them is stepped past the way a client does.
	var entries []servertest.EquipmentEntry
	for gotID, data := range c.Stream() {
		c.T.Helper()
		switch gotID {
		case protocol.IDPlayEntityEquipment:
			r := protocol.NewReader(data)
			if got := r.ReadVarInt(); got != id {
				c.T.Fatalf("equipment for entity %d while reading the spawn of %d", got, id)
			}
			for {
				raw := r.ReadU8()
				stack, _ := r.ReadSlot()
				entries = append(entries, servertest.EquipmentEntry{Slot: raw & 0x7F, Stack: stack})
				if raw&0x80 == 0 {
					break
				}
			}
			if err := r.Err(); err != nil {
				c.T.Fatalf("spawn equipment: %v", err)
			}
		case protocol.IDPlayUpdateAttributes:
			if got := protocol.NewReader(data).ReadVarInt(); got != id {
				c.T.Fatalf("attributes for entity %d while reading the spawn of %d", got, id)
			}
			return id, entries
		}
		if servertest.HostileSkip(gotID) {
			continue
		}
		c.T.Fatalf("unexpected packet 0x%02X in the spawn grouping of %d", gotID, id)
	}
	c.T.Fatalf("the spawn grouping of %d never finished", id)
	return 0, nil
}

// helmetItemIDs is the six helmets Mob.getEquipmentForSlot hands the spawn's
// material roll: leather, copper, golden, chainmail, iron, diamond.
func helmetItemIDs(t *testing.T) map[int32]bool {
	t.Helper()
	helmets := map[int32]bool{}
	for _, name := range []string{
		"minecraft:leather_helmet", "minecraft:copper_helmet", "minecraft:golden_helmet",
		"minecraft:chainmail_helmet", "minecraft:iron_helmet", "minecraft:diamond_helmet",
	} {
		helmets[servertest.MustItemID(t, name)] = true
	}
	return helmets
}

// zombieToolItemIDs is the three iron tools Zombie's own tool roll arms a
// zombie with: the sword, the spear and the shovel.
func zombieToolItemIDs(t *testing.T) map[int32]bool {
	t.Helper()
	tools := map[int32]bool{}
	for _, name := range []string{"minecraft:iron_sword", "minecraft:iron_spear", "minecraft:iron_shovel"} {
		tools[servertest.MustItemID(t, name)] = true
	}
	return tools
}

// slotsAbove names the armour slots the wire numbers above the one given.
func slotsAbove(slot uint8) []uint8 {
	switch slot {
	case protocol.EquipFeet:
		return []uint8{protocol.EquipLegs, protocol.EquipChest, protocol.EquipHead}
	case protocol.EquipLegs:
		return []uint8{protocol.EquipChest, protocol.EquipHead}
	case protocol.EquipChest:
		return []uint8{protocol.EquipHead}
	}
	return nil
}

// TestSpawnedZombiesArriveWearingArmour checks the spawn's own roll over the
// wire: zombies stood up where the difficulty is hard and the moon is full
// sometimes arrive wearing a helmet of one of the six materials — always the
// head first, no piece standing without the ones above it — and sometimes
// holding nothing but an iron sword, spear or shovel. The seeds the sixty
// spawns mint are the same every run, so the share is a fixture, not a
// chance.
func TestSpawnedZombiesArriveWearingArmour(t *testing.T) {
	t.Parallel()
	cfg := servertest.HostileTestConfig()
	srv, connect := servertest.Start(t, cfg)
	srv.SetDifficulty(rules.DifficultyHard)
	srv.SetDayTime(fullMoonMidnight)
	c := connect().Join("Quartermaster")
	c.Settle()

	// The loop stands stopped while the spawns are read, so no tick's
	// telling about an earlier body lands between the packets of a later
	// one's grouping.
	srv.StopTick()

	helmets := helmetItemIDs(t)
	tools := zombieToolItemIDs(t)
	const spawns = 60
	var armoured, tooled int
	for i := range spawns {
		srv.SpawnMob("minecraft:zombie", 4.5+float64(i%10), 64, 8.5+float64(i/10))
		id, entries := spawnGroup(c, protocol.EntityTypeZombie)
		if id == 0 {
			t.Fatal("a spawned zombie carries no entity ID")
		}
		if head := servertest.EquipEntry(entries, protocol.EquipHead); !head.Empty() {
			if !helmets[head.ItemID] {
				name, _ := registry.ItemName(head.ItemID)
				t.Errorf("a spawned zombie wears %s on its head, want one of the six helmets", name)
			}
			armoured++
			// The roll dresses the body head down, so no piece it
			// puts on stands without its head.
			for _, slot := range []uint8{protocol.EquipFeet, protocol.EquipLegs, protocol.EquipChest} {
				if piece := servertest.EquipEntry(entries, slot); !piece.Empty() {
					for _, above := range slotsAbove(slot) {
						if servertest.EquipEntry(entries, above).Empty() {
							t.Errorf("a spawned zombie wears a piece at slot %d with the slot %d above it bare", slot, above)
						}
					}
				}
			}
		} else {
			for _, piece := range entries {
				if piece.Slot != protocol.EquipMainHand {
					t.Errorf("a spawned zombie with no helmet carries slot %d, want no armour below a bare head", piece.Slot)
				}
			}
		}
		if held := servertest.EquipEntry(entries, protocol.EquipMainHand); !held.Empty() {
			if !tools[held.ItemID] {
				name, _ := registry.ItemName(held.ItemID)
				t.Errorf("a spawned zombie holds %s, want the iron sword, spear or shovel", name)
			}
			tooled++
		}
	}
	if armoured == 0 {
		t.Errorf("none of %d spawned zombies wore armour, want some dressed by the hard moonlit roll", spawns)
	}
	if tooled == 0 {
		t.Errorf("none of %d spawned zombies held an iron tool, want some armed by the hard roll", spawns)
	}
}

// TestAnArmouredZombieTakesLessOfABlow checks the armour's share of a hit:
// the same whole sword blow a bare spawn takes the whole of, a zombie in a
// summoned iron chestplate takes the armour formula's share off — the
// chestplate's six points on the zombie's natural two.
func TestAnArmouredZombieTakesLessOfABlow(t *testing.T) {
	t.Parallel()
	cfg := servertest.HostileTestConfig()
	cfg.GameMode = "creative" // bids no bite and no hunt noise between the blows
	srv, connect := servertest.Start(t, cfg, servertest.OpsListing(t, "Fencer"))
	srv.SetDayTime(18000) // midnight, whose dark keeps the daylight burn out
	c := connect()
	attacker := c.JoinAs("Fencer")
	c.MoveToGround(4.5, 64, 5.5, true)
	c.Settle()

	srv.SpawnMob("minecraft:zombie", 5.5, 64, 5.5)
	bareID, bareEntries := spawnGroup(c, protocol.EntityTypeZombie)
	if !servertest.EquipEntry(bareEntries, protocol.EquipHead).Empty() {
		c.T.Fatal("the bare zombie arrived wearing armour on the easy difficulty, want it bare")
	}
	c.ChatCommand(`summon minecraft:zombie 5.5 64 7.5 {equipment:{chest:{id:"minecraft:iron_chestplate",count:1}}}`)
	armouredID, entries := spawnGroup(c, protocol.EntityTypeZombie)
	worn := servertest.EquipEntry(entries, protocol.EquipChest)
	if worn.Empty() || worn.ItemID != servertest.MustItemID(c.T, "minecraft:iron_chestplate") {
		c.T.Fatalf("the summoned zombie's chest is %+v, want the iron chestplate", worn)
	}

	srv.GiveSlot("Fencer", player.HotbarSlot, protocol.ItemStack{
		ItemID: servertest.MustItemID(c.T, "minecraft:diamond_sword"), Count: 1,
	})
	servertest.AwaitFullSwing(t, srv, "Fencer")
	// The loop stands stopped for the striking, so no body's hunt tells
	// between the blows and the blows' packets read clean.
	srv.StopTick()
	c.Settle()

	bare := c.Strike(bareID, attacker)
	if got, want := bare, float32(20-rules.DamageAfterArmour(servertest.SwordDamage(), 2, 0)); absDiff(got, want) > 0.01 {
		c.T.Errorf("the bare zombie's health after the blow = %v, want %v (the natural two alone)", got, want)
	}
	// The blow spent the swing; the struck zombies never tick while the
	// loop stands stopped, so the recovery is wound by hand.
	servertest.RecoverByHand(t, srv, "Fencer")
	armoured := c.Strike(armouredID, attacker)
	if got, want := armoured, float32(20-rules.DamageAfterArmour(servertest.SwordDamage(), 8, 0)); absDiff(got, want) > 0.01 {
		c.T.Errorf("the armoured zombie's health after the blow = %v, want %v (the chestplate's six on the natural two)", got, want)
	}
	if armoured <= bare {
		c.T.Errorf("the armoured zombie took the blow down to %v, no less than the bare one's %v", armoured, bare)
	}
}

// absDiff is the distance between two readings, in the width a whole float
// wants to be compared with.
func absDiff(got, want float32) float64 {
	d := got - want
	if d < 0 {
		d = -d
	}
	return float64(d)
}

// TestAHelmetedZombieDoesNotBurnInTheSun checks the sun's answer to a
// helmet: a bare zombie under the noon sky catches fire the dice light,
// while one that arrived in a leather helmet never does — and the helmet
// takes the wear instead, its wear showing in the Set Equipment the change
// sends.
func TestAHelmetedZombieDoesNotBurnInTheSun(t *testing.T) {
	t.Parallel()
	cfg := servertest.HostileTestConfig()
	cfg.GameMode = "creative"

	srv, connect := servertest.Start(t, cfg, servertest.OpsListing(t, "Sunwatcher"))
	srv.SetDayTime(6000) // noon, under whose open sky the burn rolls
	c := connect().Join("Sunwatcher")
	c.Settle()

	srv.SpawnMob("minecraft:zombie", 4.5, 64, 7.5)
	bareID, _ := spawnGroup(c, protocol.EntityTypeZombie)
	c.ChatCommand(`summon minecraft:zombie 8.5 64 7.5 {equipment:{head:{id:"minecraft:leather_helmet",count:1}}}`)
	helmetedID, entries := spawnGroup(c, protocol.EntityTypeZombie)
	helmet := servertest.EquipEntry(entries, protocol.EquipHead)
	if helmet.Empty() || helmet.ItemID != servertest.MustItemID(c.T, "minecraft:leather_helmet") {
		c.T.Fatalf("the summoned zombie's head is %+v, want a leather helmet", helmet)
	}

	// The bare body's ignition is the clock the helmeted one is held
	// against — the two watches read the one stream of packets.
	helmetedFire, bareIgnited := false, false
	ignition := func(id int32, data []byte) bool {
		if id != protocol.IDPlayEntityMetadata {
			return false
		}
		m := servertest.ReadZombieMetadata(c, data)
		if m.EntityID == helmetedID && m.HasFire && m.OnFire {
			helmetedFire = true
		}
		return m.EntityID == bareID && m.HasFire && m.OnFire
	}
	bareIgnited = pumpTicks(c, srv, 400, nil, ignition)
	if !bareIgnited {
		t.Error("the bare zombie never caught fire in the noonday sun, so the watch held nothing against it")
	}
	if helmetedFire {
		t.Error("a zombie wearing a helmet caught fire in the sun, want the helmet to keep the sun off it")
	}

	// The helmet's own wear: the ticks it stood the sun off under wear it
	// a point at a time, and the change tells over the wire while the
	// helmeted body stays dark.
	helmetWorn := false
	worn := func(id int32, data []byte) bool {
		if id != protocol.IDPlayEntityEquipment {
			return false
		}
		r := protocol.NewReader(data)
		if got := r.ReadVarInt(); got != helmetedID {
			return false
		}
		for r.Err() == nil {
			raw := r.ReadU8()
			stack, _ := r.ReadSlot()
			if raw&0x7F == protocol.EquipHead && stack.Damage > 0 {
				helmetWorn = true
			}
			if raw&0x80 == 0 {
				break
			}
		}
		return helmetWorn
	}
	pumpTicks(c, srv, 400, nil, worn)
	if !helmetWorn {
		t.Error("the helmet never took wear while it kept the sun off, want the wear instead of the burn")
	}
}

// TestADamagedPieceKeepsItsWearOverTheSave checks the equipment codec's
// damage side on a worn slot: a summoned helmet that arrives already worn
// carries its wear on the wire, and the mob's entity file keeps it under the
// damage component a reload reads back.
func TestADamagedPieceKeepsItsWearOverTheSave(t *testing.T) {
	t.Parallel()
	cfg := servertest.HostileTestConfig()
	cfg.GameMode = "creative"
	cfg.LevelName = t.TempDir()

	srv, connect := servertest.Start(t, cfg, servertest.OpsListing(t, "Armourer"))
	srv.SetDayTime(18000) // midnight, whose dark keeps the daylight burn out
	c := connect().Join("Armourer")
	c.Settle()

	const wear = 7
	c.ChatCommand(`summon minecraft:zombie 4.5 64 7.5 {equipment:{head:{id:"minecraft:iron_helmet",count:1,components:{"minecraft:damage":7}}}}`)
	id, entries := spawnGroup(c, protocol.EntityTypeZombie)
	if id == 0 {
		t.Fatal("the summoned zombie carries no entity ID")
	}
	head := servertest.EquipEntry(entries, protocol.EquipHead)
	if head.ItemID != servertest.MustItemID(c.T, "minecraft:iron_helmet") || head.Damage != wear {
		c.T.Fatalf("the summoned helmet is %+v, want the iron helmet worn %d", head, wear)
	}

	srv.StopTick()
	srv.FlushMobs()
	servertest.EntityFileIs(t, cfg.LevelName, world.ChunkPos{X: 0, Z: 0}, "minecraft:zombie", func(compound nbt.Compound) {
		equipment, ok := nbt.Get[nbt.Compound](compound, "equipment")
		if !ok {
			t.Fatal("the saved zombie has no equipment")
		}
		helm, ok := nbt.Get[nbt.Compound](equipment, "head")
		if !ok {
			t.Fatal("the saved equipment has no head")
		}
		components, ok := nbt.Get[nbt.Compound](helm, "components")
		if !ok {
			t.Fatal("the saved helmet has no components")
		}
		damage, ok := nbt.Get[nbt.Int](components, "minecraft:damage")
		if !ok || damage != wear {
			t.Errorf("the saved helmet's damage = %v, %v; want %d", damage, ok, wear)
		}
	})
}

// TestABurntHelmetSendsTheHeadBreak checks the break event's wire byte: the
// sun wearing a helmet through tells watchers the head's equipment break,
// the status vanilla's entityEventForEquipmentBreak carries for HEAD.
func TestABurntHelmetSendsTheHeadBreak(t *testing.T) {
	t.Parallel()
	cfg := servertest.HostileTestConfig()
	cfg.GameMode = "creative"

	srv, connect := servertest.Start(t, cfg, servertest.OpsListing(t, "Sunburn"))
	srv.SetDayTime(6000) // noon, under whose open sky the burn rolls
	c := connect().Join("Sunburn")
	c.Settle()

	// One wear short of breaking, so the first roll of a point clears it.
	atLimit := registry.MaxDamage(servertest.MustItemID(t, "minecraft:leather_helmet")) - 1
	c.ChatCommand(fmt.Sprintf(`summon minecraft:zombie 4.5 64 7.5 {equipment:{head:{id:"minecraft:leather_helmet",count:1,components:{"minecraft:damage":%d}}}}`, atLimit))
	id, entries := spawnGroup(c, protocol.EntityTypeZombie)
	if id == 0 {
		t.Fatal("the summoned zombie carries no entity ID")
	}
	if servertest.EquipEntry(entries, protocol.EquipHead).Empty() {
		t.Fatal("the summoned zombie arrived bare-headed, so nothing can burn through")
	}

	broke := false
	var status uint8
	scan := func(gotID int32, data []byte) bool {
		if gotID != protocol.IDPlayEntityEvent {
			return false
		}
		r := protocol.NewReader(data)
		if r.ReadI32() != id {
			return false
		}
		// The watch ends on any equipment-break status, 47 through 52:
		// the point is which one the head sends, not whether one came.
		if got := r.ReadU8(); got >= 47 && got <= 52 {
			status, broke = got, true
			return true
		}
		return false
	}
	pumpTicks(c, srv, 400, nil, scan)
	if !broke {
		t.Fatal("the burnt-away helmet never sent an equipment break")
	}
	if status != 49 {
		t.Errorf("the burnt-away helmet sent break status %d, want the head's 49", status)
	}
}

// TestAHelmetsEmptyAttributeModifiersGiveNoArmour checks that a worn stack's
// own attribute_modifiers component replaces the item's default: a zombie in
// a leather helmet whose component is empty takes a blow as a bare zombie
// does, while one in a plain leather helmet takes the helmet's point off it.
// vanilla: ItemStack.forEachModifier
func TestAHelmetsEmptyAttributeModifiersGiveNoArmour(t *testing.T) {
	t.Parallel()
	cfg := servertest.HostileTestConfig()
	cfg.GameMode = "creative"
	srv, c := servertest.Join(t, cfg, "Smith", servertest.OpsListing(t, "Smith"))
	srv.SetDayTime(18000) // midnight, whose dark keeps the daylight burn out

	summon := func(x float64, tag, equipment string) int32 {
		c.ChatCommand(fmt.Sprintf(`summon minecraft:zombie %v 64 7.5 {NoAI:1b,Tags:["%s"]%s}`, x, tag, equipment))
		return c.ExpectMobSpawn(protocol.EntityTypeZombie).EntityID
	}
	bare := summon(2.5, "bare", "")
	emptied := summon(5.5, "emptied",
		`,equipment:{head:{id:"minecraft:leather_helmet",count:1,components:{"minecraft:attribute_modifiers":[]}}}`)
	plain := summon(8.5, "plain", `,equipment:{head:{id:"minecraft:leather_helmet",count:1}}`)
	// The worn pieces' modifiers go onto the map on the mobs' ticks.
	servertest.RunTicks(t, srv, 3)

	for _, tag := range []string{"bare", "emptied", "plain"} {
		c.ChatCommand(fmt.Sprintf("damage @e[type=minecraft:zombie,tag=%s,limit=1] 10 minecraft:mob_attack", tag))
	}
	c.Settle()
	health := func(id int32) float64 {
		hp, ok := srv.MobHealth(id)
		if !ok {
			t.Fatalf("zombie %d is gone", id)
		}
		return hp
	}
	// A zombie's natural two points of armour; the leather helmet adds one.
	if got, want := health(bare), 20-rules.DamageAfterArmour(10, 2, 0); absDiff(float32(got), float32(want)) > 0.01 {
		t.Errorf("bare zombie's health = %v, want %v", got, want)
	}
	if got, want := health(emptied), health(bare); got != want {
		t.Errorf("zombie in a helmet with empty attribute_modifiers has %v health, want the bare zombie's %v", got, want)
	}
	if got, want := health(plain), 20-rules.DamageAfterArmour(10, 3, 0); absDiff(float32(got), float32(want)) > 0.01 {
		t.Errorf("zombie in a plain leather helmet has %v health, want %v", got, want)
	}
}

// TestAFallingAnvilIsCutByAZombiesHelmet checks the damages_helmet cut on a
// mob: an anvil dropped the same height takes from a zombie in a carved
// pumpkin three quarters of what it takes from a bare one. Both have their
// armour zeroed and the pumpkin carries no modifiers, so only the cut tells
// them apart, and the loop is stopped so no sun or step touches their health.
// vanilla: LivingEntity.hurtServer
func TestAFallingAnvilIsCutByAZombiesHelmet(t *testing.T) {
	t.Parallel()
	srv, c, _ := servertest.JoinAs(t, servertest.MobTestConfig(), "Smith", servertest.OpsListing(t, "Smith"))

	const noArmor = `attributes:[{id:"minecraft:armor",base:0.0d}]`
	c.ChatCommand(`summon minecraft:zombie 4.5 64 7.5 {NoAI:1b,` + noArmor + `}`)
	bare := c.ExpectMobSpawn(protocol.EntityTypeZombie).EntityID
	c.ChatCommand(`summon minecraft:zombie 9.5 64 7.5 {NoAI:1b,` + noArmor +
		`,equipment:{head:{id:"minecraft:carved_pumpkin",count:1}}}`)
	capped := c.ExpectMobSpawn(protocol.EntityTypeZombie).EntityID
	c.Settle()

	srv.StopTick()
	// The stopped loop streams no chunks; hold the anvils' one in memory.
	release := srv.HoldChunk(0, 0)
	t.Cleanup(release)

	c.ChatCommand("setblock 4 67 7 minecraft:anvil")
	c.ChatCommand("setblock 9 67 7 minecraft:anvil")
	c.Settle()
	for range 40 {
		if srv.FallingCount() == 2 {
			break
		}
		srv.TickPhysics(1)
	}
	if srv.FallingCount() != 2 {
		t.Fatalf("%d anvils became falling bodies, want 2", srv.FallingCount())
	}
	for range 40 {
		if srv.FallingCount() == 0 {
			break
		}
		srv.TickFallings(1)
	}
	if srv.FallingCount() != 0 {
		t.Fatal("the anvils never landed")
	}

	bareHealth, ok := srv.MobHealth(bare)
	if !ok {
		t.Fatal("the bare zombie is gone")
	}
	cappedHealth, ok := srv.MobHealth(capped)
	if !ok {
		t.Fatal("the capped zombie is gone")
	}
	bareTaken, cappedTaken := 20-bareHealth, 20-cappedHealth
	if bareTaken <= 0 {
		t.Fatalf("the anvil took nothing from the bare zombie (health %v)", bareHealth)
	}
	if want := 0.75 * bareTaken; math.Abs(cappedTaken-want) > 1e-4 {
		t.Errorf("the capped zombie took %v, want three quarters of the bare one's %v: %v", cappedTaken, bareTaken, want)
	}
}
