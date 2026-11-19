package mobs

import (
	"math"
	"testing"

	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/internal/server/player"
	"github.com/golem-mc/golem/internal/server/servertest"
	"github.com/golem-mc/golem/registry"
)

const goatOp = "Herder"

// goatWatch is what a client heard of one goat during a run of ticks.
type goatWatch struct {
	lowered, raised int
	hurt            bool
	motionX         float64
	motion          bool
	sounds          map[string]int
}

func readGoat(t *testing.T, c *servertest.Client, goat int32, w *goatWatch, data []byte, id int32) {
	t.Helper()
	switch id {
	case protocol.IDPlayEntityEvent:
		r := protocol.NewReader(data)
		if r.ReadI32() != goat {
			return
		}
		switch r.ReadU8() {
		case protocol.EntityStatusGoatLowerHead:
			w.lowered++
		case protocol.EntityStatusGoatRaiseHead:
			w.raised++
		}
	case protocol.IDPlaySoundEffect:
		name, _, _, _, _, _, _ := servertest.ReadAmbient(t, data)
		if w.sounds == nil {
			w.sounds = map[string]int{}
		}
		w.sounds[name]++
	case protocol.IDPlayDamageEvent:
		w.hurt = true
	case protocol.IDPlaySetEntityMotion:
		r := protocol.NewReader(data)
		if r.ReadVarInt() == c.PlayerEntityID {
			vx, _, _ := r.ReadLpVec3()
			w.motion, w.motionX = true, min(w.motionX, vx)
		}
	}
}

// TestAGoatPreparesAndRamsAPlayer: a goat eight blocks from a survival
// player with no ram cooldown walks to a run-up cell, lowers its head
// (entity event 58), charges, hurts the player and shoves them away from
// the goat, then raises its head (59).
func TestAGoatPreparesAndRamsAPlayer(t *testing.T) {
	t.Parallel()
	srv, c := servertest.BreedingPlayer(t, "survival", "Rammed", "", 0)
	servertest.HoldTicks(t, srv)
	id := srv.SpawnMob("minecraft:goat", 8.5, 64, 0.5)
	c.ExpectMobSpawn(protocol.EntityTypeGoat)
	srv.SetGoatRamCooldown(id, 0)
	var w goatWatch
	c.ReadTicks(srv, 400, func(packetID int32, data []byte) bool {
		readGoat(t, c, id, &w, data, packetID)
		return w.motion && w.raised > 0
	})
	if w.lowered == 0 {
		t.Error("the goat never lowered its head (entity event 58)")
	}
	if w.sounds["minecraft:entity.goat.prepare_ram"] == 0 {
		t.Error("no prepare_ram sound")
	}
	if w.sounds["minecraft:entity.goat.ram_impact"] == 0 {
		t.Error("no ram_impact sound")
	}
	if !w.hurt {
		t.Error("the ram dealt no damage")
	}
	if !w.motion || w.motionX >= -0.5 {
		t.Errorf("the player's motion x = %.2f (told %v), want a shove of more than half a block a tick away from the goat", w.motionX, w.motion)
	}
	if w.raised == 0 {
		t.Error("the goat never raised its head again (entity event 59)")
	}
}

// TestAGoatOnCooldownDoesNotRam: the ram cooldown holds the goat back.
func TestAGoatOnCooldownDoesNotRam(t *testing.T) {
	t.Parallel()
	srv, c := servertest.BreedingPlayer(t, "survival", "Rammed", "", 0)
	servertest.HoldTicks(t, srv)
	id := srv.SpawnMob("minecraft:goat", 8.5, 64, 0.5)
	c.ExpectMobSpawn(protocol.EntityTypeGoat)
	srv.SetGoatRamCooldown(id, 1000)
	var w goatWatch
	c.ReadTicks(srv, 300, func(packetID int32, data []byte) bool {
		readGoat(t, c, id, &w, data, packetID)
		return false
	})
	if w.lowered != 0 || w.hurt {
		t.Errorf("a goat on cooldown lowered its head %d times and hurt %v", w.lowered, w.hurt)
	}
}

// TestAGoatRammingABlockDropsAHorn: a goat that charges where its target
// stood and meets a log snaps a horn off (the item with an instrument
// of its tag), plays the impact and horn-break sounds, and loses the horn
// from its metadata.
func TestAGoatRammingABlockDropsAHorn(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name      string
		tag       string
		prepare   string
		instrumts []string
	}{
		{"regular", "{}", "minecraft:entity.goat.prepare_ram", []string{"ponder", "sing", "seek", "feel"}},
		{"screaming", "{IsScreamingGoat:1b}", "minecraft:entity.goat.screaming.prepare_ram", []string{"admire", "call", "yearn", "dream"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			srv, c := servertest.BreedingPlayer(t, "survival", goatOp, "", 0, servertest.OpsListing(t, goatOp))
			servertest.HoldTicks(t, srv)
			log := registry.MustState("minecraft:oak_log", map[string]string{"axis": "y"})
			srv.SetBlock(-1, 64, 0, log)
			srv.SetBlock(-1, 65, 0, log)
			c.ChatCommand("summon minecraft:goat 8.5 64 0.5 " + tc.tag)
			goat := c.ExpectMobSpawn(protocol.EntityTypeGoat)
			srv.SetGoatRamCooldown(goat.EntityID, 0)
			var w goatWatch
			var stack protocol.ItemStack
			c.ReadTicks(srv, 300, func(packetID int32, data []byte) bool {
				readGoat(t, c, goat.EntityID, &w, data, packetID)
				return w.sounds[tc.prepare] > 0
			})
			if w.sounds[tc.prepare] == 0 {
				t.Fatalf("the goat never prepared a ram; sounds %v", w.sounds)
			}
			// The charge is on: the player steps out of the way, so the goat
			// runs on into the log behind where they stood.
			c.MoveTo(0.5, 64, 6.5, 0, 0)
			c.Settle()
			dropped := false
			c.ReadTicks(srv, 200, func(packetID int32, data []byte) bool {
				readGoat(t, c, goat.EntityID, &w, data, packetID)
				if packetID == protocol.IDPlaySpawnEntity {
					drop := servertest.ParseItemSpawn(data)
					if drop.EntityID != 0 {
						c.ReadDropStack(&drop)
						stack, dropped = drop.Stack, true
					}
				}
				return dropped
			})
			if !dropped {
				t.Fatalf("no horn dropped; sounds %v", w.sounds)
			}
			horn, _ := registry.ItemID("minecraft:goat_horn")
			if stack.ItemID != horn {
				t.Fatalf("the drop is item %d, want the goat horn %d", stack.ItemID, horn)
			}
			// vanilla: ItemEntity's default pickup delay, 0
			for _, it := range srv.ItemsInDimension("minecraft:overworld") {
				if it.Stack.ItemID == horn && it.PickupDelay != 0 {
					t.Errorf("the dropped horn has pickup delay %d, want 0", it.PickupDelay)
				}
			}
			in, ok := stack.InstrumentOf()
			if !ok {
				t.Fatal("the horn carries no instrument")
			}
			reg, _ := registry.Get("minecraft:instrument")
			got := reg.Entries[in]
			found := false
			for _, name := range tc.instrumts {
				found = found || got == "minecraft:"+name+"_goat_horn"
			}
			if !found {
				t.Errorf("the instrument is %s, want one of %v", got, tc.instrumts)
			}
			c.ReadTicks(srv, 5, func(packetID int32, data []byte) bool {
				readGoat(t, c, goat.EntityID, &w, data, packetID)
				return false
			})
			if w.sounds["minecraft:entity.goat.horn_break"] == 0 {
				t.Error("no horn_break sound")
			}
			left, right := srv.GoatHornsLeft(goat.EntityID)
			if left == right {
				t.Errorf("horns left %v/%v, want exactly one lost", left, right)
			}
		})
	}
}

// TestAGoatShowsItsVariantAndHorns: the spawn metadata carries the
// screaming flag and a missing horn; a plain goat tells nothing.
func TestAGoatShowsItsVariantAndHorns(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		tag  string
		want map[uint8]bool
	}{
		{"{IsScreamingGoat:1b}", map[uint8]bool{protocol.MetadataIndexGoatScreaming: true}},
		{"{HasLeftHorn:0b}", map[uint8]bool{protocol.MetadataIndexGoatLeftHorn: false}},
		{"{HasRightHorn:0b,IsScreamingGoat:0b}", map[uint8]bool{protocol.MetadataIndexGoatRightHorn: false}},
		{"{}", nil},
	} {
		t.Run(tc.tag, func(t *testing.T) {
			t.Parallel()
			srv, c := servertest.BreedingPlayer(t, "survival", goatOp, "", 0, servertest.OpsListing(t, goatOp))
			servertest.HoldTicks(t, srv)
			c.ChatCommand("summon minecraft:goat 5.5 64 5.5 " + tc.tag)
			goat := c.ExpectMobSpawn(protocol.EntityTypeGoat)
			if len(goat.GoatFlags) != len(tc.want) {
				t.Errorf("%s: told %v, want %v", tc.tag, goat.GoatFlags, tc.want)
			}
			for index, v := range tc.want {
				if got, told := goat.GoatFlags[index]; !told || got != v {
					t.Errorf("%s: index %d = %v (told %v), want %v", tc.tag, index, got, told, v)
				}
			}
		})
	}
}

// TestMilkingAGoatFillsTheBucket: a grown goat fills the bucket and a
// bystander hears its milk sound, the screaming goat's own.
func TestMilkingAGoatFillsTheBucket(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		tag, sound string
	}{
		{"{}", "minecraft:entity.goat.milk"},
		{"{IsScreamingGoat:1b}", "minecraft:entity.goat.screaming.milk"},
	} {
		t.Run(tc.tag, func(t *testing.T) {
			t.Parallel()
			srv, connect := servertest.Start(t, servertest.MobTestConfig(), servertest.OpsListing(t, goatOp))
			c := connect().Join(goatOp)
			c.MoveToGround(0.5, 64, 0.5, true)
			c.Settle()
			srv.GiveSlot(goatOp, player.HotbarSlot, protocol.ItemStack{ItemID: servertest.MustItemID(t, "minecraft:bucket"), Count: 1})
			watcher := connect().Join("Watcher")
			watcher.Settle()
			c.Settle()
			servertest.HoldTicks(t, srv)
			c.ChatCommand("summon minecraft:goat 1.5 64 0.5 " + tc.tag)
			goat := c.ExpectMobSpawn(protocol.EntityTypeGoat)
			watcher.ExpectMobSpawn(protocol.EntityTypeGoat)
			c.UseOn(goat.EntityID)
			milk, _ := registry.ItemID("minecraft:milk_bucket")
			_, stack := c.ExpectContainerSlot()
			if stack.ItemID != milk {
				t.Errorf("the bucket became item %d, want the milk bucket %d", stack.ItemID, milk)
			}
			var w goatWatch
			watcher.ReadTicks(srv, 3, func(packetID int32, data []byte) bool {
				readGoat(t, watcher, goat.EntityID, &w, data, packetID)
				return false
			})
			if w.sounds[tc.sound] != 1 {
				t.Errorf("the watcher heard %v, want %s once", w.sounds, tc.sound)
			}
		})
	}
}

// TestAGoatFindingNoTargetTakesTheFailCooldown: the ram behaviour looks
// once when the cooldown runs out, and a goat that sees nobody takes the
// failed-ram cooldown and sends the raise head event.
// vanilla: PrepareRamNearestTarget.stop
func TestAGoatFindingNoTargetTakesTheFailCooldown(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name, tag string
		want      int
	}{
		{"regular", "{}", 600},
		{"screaming", "{IsScreamingGoat:1b}", 100},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			srv, c := servertest.BreedingPlayer(t, "survival", goatOp, "", 0, servertest.OpsListing(t, goatOp))
			servertest.HoldTicks(t, srv)
			c.ChatCommand("summon minecraft:goat 40.5 64 0.5 " + tc.tag)
			goat := c.ExpectMobSpawn(protocol.EntityTypeGoat)
			srv.SetGoatRamCooldown(goat.EntityID, 0)
			var w goatWatch
			// The sensor's first scan comes within its 20-tick period, and the
			// goat has nothing to look at before it.
			c.ReadTicks(srv, 25, func(packetID int32, data []byte) bool {
				readGoat(t, c, goat.EntityID, &w, data, packetID)
				return false
			})
			if w.raised != 1 {
				t.Errorf("the watcher saw the raise head event %d times, want once", w.raised)
			}
			if got := srv.GoatRamCooldown(goat.EntityID); got < tc.want-25 || got > tc.want {
				t.Errorf("the ram cooldown after a look at nobody = %d, want about %d", got, tc.want)
			}
		})
	}
}

// TestAGoatChargesOnTheTwentyFirstTickAtTheRunUpCell: the head lowers on the
// first tick at the cell and the prepare sound plays twenty ticks later.
// vanilla: PrepareRamNearestTarget.tick
func TestAGoatChargesOnTheTwentyFirstTickAtTheRunUpCell(t *testing.T) {
	t.Parallel()
	srv, c := servertest.BreedingPlayer(t, "survival", "Rammed", "", 0)
	servertest.HoldTicks(t, srv)
	id := srv.SpawnMob("minecraft:goat", 8.5, 64, 0.5)
	c.ExpectMobSpawn(protocol.EntityTypeGoat)
	srv.SetGoatRamCooldown(id, 0)
	loweredAt, soundAt := -1, -1
	for tick := 1; tick <= 400 && soundAt < 0; tick++ {
		var w goatWatch
		c.ReadTicks(srv, 1, func(packetID int32, data []byte) bool {
			readGoat(t, c, id, &w, data, packetID)
			return false
		})
		if w.lowered > 0 && loweredAt < 0 {
			loweredAt = tick
		}
		if w.sounds["minecraft:entity.goat.prepare_ram"] > 0 {
			soundAt = tick
		}
	}
	if loweredAt < 0 || soundAt < 0 {
		t.Fatalf("lowered at %d, prepare sound at %d", loweredAt, soundAt)
	}
	if soundAt-loweredAt != 20 {
		t.Errorf("the prepare sound came %d ticks after the head lowered, want 20", soundAt-loweredAt)
	}
}

// goatRamOnto runs a goat 8 blocks south of a survival player, optionally
// holding a raised shield, and reports the player's last shove along z, the
// damage type ID of any damage event, and their health.
func goatRamOnto(t *testing.T, shield bool, speed int32) (shoveZ float64, damageType int32, health float64) {
	t.Helper()
	return goatRamOntoIn(t, false, shield, speed)
}

// goatRamOntoIn is goatRamOnto for a player whose client may still be loading.
func goatRamOntoIn(t *testing.T, unloaded, shield bool, speed int32) (shoveZ float64, damageType int32, health float64) {
	t.Helper()
	cfg := servertest.MobTestConfig()
	cfg.GameMode = "survival"
	srv, c := servertest.Join(t, cfg, goatOp, servertest.OpsListing(t, goatOp))
	c.MoveToGround(0.5, 64, 0.9, true)
	if shield {
		srv.GiveSlot(goatOp, player.OffHandSlot, protocol.ItemStack{ItemID: servertest.MustItemID(t, "minecraft:shield"), Count: 1})
		c.UseItemRightClicks(protocol.HandOff)
	}
	c.Settle()
	if unloaded {
		srv.HoldPlayerUnloaded(goatOp)
	}
	servertest.HoldTicks(t, srv)
	c.ChatCommand("summon minecraft:goat 0.5 64 8.5")
	goat := c.ExpectMobSpawn(protocol.EntityTypeGoat)
	srv.SetGoatRamCooldown(goat.EntityID, 0)
	if speed > 0 {
		srv.GiveMobEffect(goat.EntityID, "minecraft:speed", 100000, speed-1)
	}
	var w goatWatch
	var moved bool
	c.ReadTicks(srv, 400, func(packetID int32, data []byte) bool {
		switch packetID {
		case protocol.IDPlaySetEntityMotion:
			r := protocol.NewReader(data)
			if r.ReadVarInt() == c.PlayerEntityID {
				_, _, vz := r.ReadLpVec3()
				moved = true
				if math.Abs(vz) > math.Abs(shoveZ) {
					shoveZ = vz
				}
			}
		case protocol.IDPlayDamageEvent:
			r := protocol.NewReader(data)
			if r.ReadVarInt() == c.PlayerEntityID {
				damageType = r.ReadVarInt()
			}
		}
		readGoat(t, c, goat.EntityID, &w, data, packetID)
		return moved && w.hurt && w.raised > 0
	})
	return shoveZ, damageType, srv.PlayerHealth(goatOp)
}

// TestAGoatRamHurtsWithTheNoAggroType: the ram's damage is
// mob_attack_no_aggro, and a shield turned toward the goat takes the blow
// but still halves the shove rather than cancelling it.
// vanilla: RamTarget.tick
func TestAGoatRamHurtsWithTheNoAggroType(t *testing.T) {
	t.Parallel()
	open, typ, hp := goatRamOnto(t, false, 0)
	if want := registry.MustID("minecraft:damage_type", "minecraft:mob_attack_no_aggro"); typ != want {
		t.Errorf("the ram's damage type ID = %d, want %d (mob_attack_no_aggro)", typ, want)
	}
	if hp >= 20 {
		t.Errorf("health after an open ram = %v, want it hurt", hp)
	}
	shielded, _, hp2 := goatRamOnto(t, true, 0)
	if hp2 != 20 {
		t.Errorf("health behind the shield = %v, want 20", hp2)
	}
	if open >= -1.5*0.546 || shielded >= -0.6*0.546 || shielded <= open*0.7 {
		t.Errorf("shoves along z open %.2f, shielded %.2f, want both pushed away and the shielded one about half", open, shielded)
	}
}

// TestAGoatRamShoveOfAPlayerStillLoadingIsHalvedByTheShield: a hurt on a
// client that is still loading returns before the shield is asked, but the
// ram still asks it for the shove.
// vanilla: RamTarget.tick applyItemBlocking
func TestAGoatRamShoveOfAPlayerStillLoadingIsHalvedByTheShield(t *testing.T) {
	t.Parallel()
	open, _, _ := goatRamOntoIn(t, true, false, 0)
	shielded, _, _ := goatRamOntoIn(t, true, true, 0)
	if open >= -1.5*0.546 || shielded >= -0.6*0.546 || shielded <= open*0.7 {
		t.Errorf("unloaded shoves along z open %.2f, shielded %.2f, want the shielded one about half", open, shielded)
	}
}

// TestAGoatRamShovesAMobItCannotHurt: the shove follows the hurt whether or
// not it landed, and the victim's new motion is sent.
// vanilla: RamTarget.tick
func TestAGoatRamShovesAMobItCannotHurt(t *testing.T) {
	t.Parallel()
	cfg := servertest.MobTestConfig()
	cfg.GameMode = "creative"
	srv, c := servertest.Join(t, cfg, goatOp, servertest.OpsListing(t, goatOp))
	c.MoveToGround(0.5, 64, 0.9, true)
	c.Settle()
	servertest.HoldTicks(t, srv)
	c.ChatCommand("summon minecraft:cow 0.5 64 2.5")
	cow := c.ExpectMobSpawn(protocol.EntityTypeCow)
	srv.SetMobInvulnerable(cow.EntityID)
	c.ChatCommand("summon minecraft:goat 0.5 64 8.5")
	goat := c.ExpectMobSpawn(protocol.EntityTypeGoat)
	srv.SetGoatRamCooldown(goat.EntityID, 0)
	var shoved bool
	c.ReadTicks(srv, 400, func(packetID int32, data []byte) bool {
		if packetID == protocol.IDPlaySetEntityMotion {
			r := protocol.NewReader(data)
			if r.ReadVarInt() == cow.EntityID {
				_, _, vz := r.ReadLpVec3()
				shoved = shoved || vz < -0.5
			}
		}
		return shoved
	})
	if !shoved {
		t.Error("the cow the goat rammed but could not hurt was never shoved away")
	}
}

// TestAGoatRamShoveGrowsWithSpeed: each speed level adds a quarter to the
// ram's speed factor.
// vanilla: RamTarget.tick speedBoostPower
func TestAGoatRamShoveGrowsWithSpeed(t *testing.T) {
	t.Parallel()
	plain, _, _ := goatRamOnto(t, false, 0)
	fast, _, _ := goatRamOnto(t, false, 4)
	// Speed IV scales the pace by 1.8 (factor 1.78) and adds 0.25*4 on top, so
	// 2.78 against 0.99 before the force of 2.5; the pace alone gives 4.5.
	if fast >= -6*0.546 {
		t.Errorf("shove with speed IV %.2f against %.2f plain, want beyond %v after travel", fast, plain, -6*0.546)
	}
}

// TestTwoWheatFedGoatsBreedACalf: wheat is in goat_food, so fed goats walk
// together and bear a calf.
// vanilla: GoatAi.getTemptations, Animal.spawnChildFromBreeding
func TestTwoWheatFedGoatsBreedACalf(t *testing.T) {
	t.Parallel()
	srv, c := servertest.BreedingPlayer(t, "survival", "Rancher", "minecraft:wheat", 64)
	srv.SpawnMob("minecraft:goat", 1.5, 64, 2.5)
	srv.SpawnMob("minecraft:goat", 2.5, 64, 2.5)
	g1 := c.ExpectMobSpawn(protocol.EntityTypeGoat)
	g2 := c.ExpectMobSpawn(protocol.EntityTypeGoat)
	c.UseOn(g1.EntityID)
	c.AwaitEntityEvent(g1.EntityID, loveHearts)
	c.UseOn(g2.EntityID)
	c.AwaitEntityEvent(g2.EntityID, loveHearts)
	kind, _ := c.AwaitNextSpawn()
	if kind != protocol.EntityTypeExperienceOrb {
		t.Fatalf("the first arrival was entity type %d, want the experience orb", kind)
	}
	calf := c.AwaitSpawn(protocol.EntityTypeGoat)
	if calf.EntityID == g1.EntityID || calf.EntityID == g2.EntityID {
		t.Fatalf("the born goat (id %d) is a parent come again", calf.EntityID)
	}
	if flag := servertest.MetaOf(c.AwaitMetadataFor(calf.EntityID), protocol.MetadataIndexBaby); flag == nil || !flag.FlagVal {
		t.Error("the bred goat is not a baby")
	}
}

// TestAGoatSpawnEggOnAGoatMakesACalf: the goat's egg is a spawn egg like the
// rest, and used on a goat it stands a baby.
func TestAGoatSpawnEggOnAGoatMakesACalf(t *testing.T) {
	t.Parallel()
	srv, c := servertest.EggPlayer(t, "survival", "Breeder", "minecraft:goat_spawn_egg", 1)
	id := srv.SpawnMob("minecraft:goat", 1.5, 64, 0.5)
	c.ExpectMobSpawn(protocol.EntityTypeGoat)
	c.UseOn(id)
	baby := c.ExpectMobSpawn(protocol.EntityTypeGoat)
	if !baby.HasBaby {
		t.Error("the egg used on a goat made a grown goat, want a calf")
	}
}
