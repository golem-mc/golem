package mobs

import (
	"github.com/golem-mc/golem/internal/server/servertest"

	"testing"

	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/internal/server/player"
	"github.com/golem-mc/golem/internal/server/world"
	"github.com/golem-mc/golem/registry"
)

// The mooshroom, checked against the decompiled MushroomCow against
// the cow it extends: the bowl vanilla's mobInteract fills with mushroom
// stew, the shears that turn it into a cow with the five mushrooms of its
// variant, the Type the brown is summoned and saved under, and the breeding
// that passes the variant on.

// mooshroomShearSkip is the background a shear's reading steps over: the
// world's stream, everything a moving body sends, and the pairing packets of
// the bodies it is not reading whole — the drops' stacks reach it inside
// readDropStack, and the new cow's state and pace are the world's, not the
// shearing's.
func mooshroomShearSkip(id int32) bool {
	switch id {
	case protocol.IDPlayUpdateTime, protocol.IDPlayUpdateLight,
		protocol.IDPlayBlockUpdate, protocol.IDPlaySectionBlocks, protocol.IDPlayChunkData,
		protocol.IDPlayChunkBatchStart, protocol.IDPlayChunkBatchFinished,
		protocol.IDPlayCenterChunk, protocol.IDPlayWorldEvent,
		protocol.IDPlayEntityPosition, protocol.IDPlayEntityPositionRot,
		protocol.IDPlayEntityRotation, protocol.IDPlayHeadRotation,
		protocol.IDPlayTeleportEntity, protocol.IDPlayContainerSlot,
		protocol.IDPlayEntityEquipment, protocol.IDPlayEntityAnimation,
		protocol.IDPlaySetEntityMotion, protocol.IDPlaySystemChat,
		protocol.IDPlayPickupItem, protocol.IDPlayPingResponse,
		protocol.IDPlayEntityEvent, protocol.IDPlayDamageEvent,
		protocol.IDPlayHurtAnimation, protocol.IDPlayEntityMetadata,
		protocol.IDPlayUpdateAttributes, protocol.IDPlayLevelParticles:
		return true
	}
	return false
}

// TestBowlOnAMooshroomFillsWithMushroomStew checks MushroomCow.mobInteract's
// bowl branch: an empty bowl on a grown mooshroom is filled with one
// mushroom stew, the milking plays from the mooshroom's own body, and the
// mooshroom is still standing there afterwards. The bucket after it answers
// AbstractCow's mobInteract behind it: the same milking a cow gives.
func TestBowlOnAMooshroomFillsWithMushroomStew(t *testing.T) {
	t.Parallel()
	srv, c := servertest.Join(t, servertest.MobTestConfig(), "Filler")
	c.MoveToGround(2.5, 64, 5.5, true)
	c.Settle()

	bowl := servertest.ItemIDOrDie(t, "minecraft:bowl")
	stew := servertest.ItemIDOrDie(t, "minecraft:mushroom_stew")
	bucket := servertest.ItemIDOrDie(t, "minecraft:bucket")
	milk := servertest.ItemIDOrDie(t, "minecraft:milk_bucket")
	srv.GiveSlot("Filler", player.HotbarSlot, protocol.ItemStack{ItemID: bowl, Count: 1})
	c.Settle()

	srv.SpawnMob("minecraft:mooshroom", 5.5, 64, 5.5)
	// The red variant is the metadata client's default, and a spawn says
	// a default by leaving it out — the pairing below reads every field it
	// carries, so reaching its end says the variant was not among them.
	mooshroom := c.ExpectMobSpawn(protocol.EntityTypeMooshroom)

	c.UseOn(mooshroom.EntityID)
	slot, stack := c.ExpectContainerSlot()
	if slot != int16(player.HotbarSlot) {
		t.Errorf("the stew went to slot %d, want the hand's %d", slot, player.HotbarSlot)
	}
	if stack.ItemID != stew || stack.Count != 1 {
		t.Errorf("the filled bowl = %+v, want one mushroom stew (item %d)", stack, stew)
	}
	if got := srv.InventorySlot("Filler", player.HotbarSlot); got.ItemID != stew || got.Count != 1 {
		t.Errorf("the server's slot holds %+v, want the mushroom stew", got)
	}
	// vanilla: entity.mooshroom.milk, played from the body in the neutral
	// category a mob's own voice speaks in.
	stewSounds := 0
	c.SettleAndCollect(func(id int32, data []byte) {
		if id != protocol.IDPlaySoundEffect {
			return
		}
		name, category, x, y, z, volume, _ := servertest.ReadAmbient(t, data)
		switch name {
		case "minecraft:entity.mooshroom.milk":
			stewSounds++
			if category != protocol.SoundCategoryNeutral {
				t.Errorf("the mooshroom's milking played in category %d, want the neutral its voice speaks in", category)
			}
			if x != 44 || y != 512 || z != 44 {
				t.Errorf("the mooshroom's milking played at (%d,%d,%d), want its feet (44,512,44)", x, y, z)
			}
			if volume != 1 {
				t.Errorf("the mooshroom's milking played at volume %v, want 1", volume)
			}
		case "minecraft:entity.cow.milk":
			t.Error("the stew's milking played the cow's sound, want the mooshroom's own")
		}
	})
	if stewSounds != 1 {
		t.Errorf("the bowl played %d mooshroom milkings, want one", stewSounds)
	}

	// The bucket behind the bowl: AbstractCow.mobInteract's milk bucket.
	srv.GiveSlot("Filler", player.HotbarSlot, protocol.ItemStack{ItemID: bucket, Count: 1})
	c.Settle()
	c.UseOn(mooshroom.EntityID)
	_, stack = c.ExpectContainerSlot()
	if stack.ItemID != milk || stack.Count != 1 {
		t.Errorf("the filled bucket = %+v, want one milk bucket (item %d)", stack, milk)
	}
	if got := srv.InventorySlot("Filler", player.HotbarSlot); got.ItemID != milk {
		t.Errorf("the server's slot holds item %d, want the milk bucket %d", got.ItemID, milk)
	}
	if got := srv.MobCount(); got != 1 {
		t.Errorf("the world holds %d mobs after the bowl and the bucket, want the one milked mooshroom", got)
	}
}

// TestShearingAMooshroomTurnsItIntoACow checks MushroomCow.shear: shears
// turn a mooshroom into a cow — the old body out of the world, a cow in its
// place — with the five mushrooms of minecraft:shearing/mooshroom/red laid
// where it stood, the shear's own sound, and a wear paid by the shears.
func TestShearingAMooshroomTurnsItIntoACow(t *testing.T) {
	t.Parallel()
	srv, c := servertest.Join(t, servertest.MobTestConfig(), "Shearer")
	c.MoveToGround(2.5, 64, 5.5, true)
	c.Settle()

	shears := servertest.ItemIDOrDie(t, "minecraft:shears")
	red := servertest.ItemIDOrDie(t, "minecraft:red_mushroom")
	srv.GiveSlot("Shearer", player.HotbarSlot, protocol.ItemStack{ItemID: shears, Count: 1})
	c.Settle()

	srv.SpawnMob("minecraft:mooshroom", 5.5, 64, 5.5)
	mooshroom := c.ExpectMobSpawn(protocol.EntityTypeMooshroom)

	c.UseOn(mooshroom.EntityID)
	var drops []servertest.ItemDrop
	shearSound, cowSpawned, mooshroomGone := false, false, false
	cowID := int32(0)
	c.ReadTicks(srv, 200, func(gotID int32, data []byte) bool {
		r := protocol.NewReader(data)
		switch gotID {
		case protocol.IDPlaySpawnEntity:
			id := r.ReadVarInt()
			r.ReadUUID()
			switch got := r.ReadVarInt(); got {
			case protocol.EntityTypeCow:
				if id == mooshroom.EntityID {
					t.Fatal("the cow arrived under the mooshroom's own entity ID")
				}
				cowID, cowSpawned = id, true
			case protocol.EntityTypeItem:
				drop := servertest.ParseItemSpawn(data)
				c.ReadDropStack(&drop)
				drops = append(drops, drop)
			}
		case protocol.IDPlayEntitySoundEffect:
			shearSound = shearSound || servertest.SoundIs(data, "minecraft:entity.mooshroom.shear")
		case protocol.IDPlayRemoveEntities:
			for count := r.ReadVarInt(); count > 0; count-- {
				if r.ReadVarInt() == mooshroom.EntityID {
					mooshroomGone = true
				}
			}
		default:
			if !mooshroomShearSkip(gotID) {
				t.Fatalf("unexpected packet 0x%02X from a shearing", gotID)
			}
		}
		return len(drops) == 5 && shearSound && cowSpawned && mooshroomGone
	})
	if got := len(drops); got != 5 {
		t.Fatalf("the shearing dropped %d stacks, want vanilla's five mushrooms", got)
	}
	for _, drop := range drops {
		if drop.Stack.ItemID != red || drop.Stack.Count != 1 {
			t.Errorf("a shearing drop = %+v, want one red mushroom (item %d)", drop.Stack, red)
		}
	}
	if !shearSound {
		t.Error("the shearing never played entity.mooshroom.shear")
	}
	if !cowSpawned || !mooshroomGone {
		t.Errorf("the conversion gave a cow (%v) and removed the mooshroom (%v), want both", cowSpawned, mooshroomGone)
	}

	// The world's own answer: one cow standing where the mooshroom was, and
	// the five mushrooms it shed lying around it.
	if got := srv.MobCount(); got != 1 {
		t.Fatalf("the world holds %d mobs after the shearing, want the one cow", got)
	}
	mob := srv.Mobs()[0]
	if mob.Kind != "minecraft:cow" || mob.ID != cowID {
		t.Errorf("the world holds a %s of ID %d, want the cow of ID %d the wire showed", mob.Kind, mob.ID, cowID)
	}
	if mob.X != 5.5 || mob.Z != 5.5 {
		t.Errorf("the cow stands at (%v,%v), want the mooshroom's (5.5,5.5)", mob.X, mob.Z)
	}
	var reds int
	for _, it := range srv.ItemsInDimension(world.DimensionName) {
		if it.Item == "minecraft:red_mushroom" {
			reds += int(it.Count)
		}
	}
	if reds != 5 {
		t.Errorf("the ground holds %d red mushrooms, want vanilla's five", reds)
	}

	// The shears paid a wear of their own.
	after := srv.InventorySlot("Shearer", player.HotbarSlot)
	if after.ItemID != shears {
		t.Errorf("the held slot holds item %d after the shearing, want the shears", after.ItemID)
	}
}

// mooshroomParticle reads a Level Particles packet's particle type, the
// first field; its options are not read.
func mooshroomParticle(t *testing.T, data []byte) int32 {
	t.Helper()
	r := protocol.NewReader(data)
	return r.ReadVarInt()
}

// vanilla: MushroomCow.mobInteract
func TestBrownMooshroomFlowerMakesOneSuspiciousStew(t *testing.T) {
	t.Parallel()
	for _, mode := range []string{"survival", "creative"} {
		t.Run(mode, func(t *testing.T) {
			cfg := servertest.MobTestConfig()
			cfg.GameMode = mode
			srv, c := servertest.Join(t, cfg, "Florist", servertest.OpsListing(t, "Florist"))
			c.MoveToGround(2.5, 64, 5.5, true)
			c.Settle()
			servertest.HoldTicks(t, srv)
			c.ChatCommand(`summon minecraft:mooshroom 5.5 64 5.5 {Type:"brown",NoAI:1b}`)
			cow := c.ExpectMobSpawn(protocol.EntityTypeMooshroom)
			flower := servertest.ItemIDOrDie(t, "minecraft:allium")
			srv.GiveSlot("Florist", player.HotbarSlot, protocol.ItemStack{ItemID: flower, Count: 3})
			c.Settle()
			for click := range 2 {
				if click == 1 {
					flower = servertest.ItemIDOrDie(t, "minecraft:poppy")
					srv.GiveSlot("Florist", player.HotbarSlot, protocol.ItemStack{ItemID: flower, Count: 3})
					c.Settle()
				}
				c.UseOn(cow.EntityID)
				eats, particles := 0, 0
				c.SettleAndCollect(func(id int32, data []byte) {
					if id == protocol.IDPlaySoundEffect {
						name, category, x, y, z, volume, pitch := servertest.ReadAmbient(t, data)
						if name == "minecraft:entity.mooshroom.eat" {
							eats++
							if category != protocol.SoundCategoryNeutral || x != 44 || y != 512 || z != 44 || volume != 2 || pitch != 1 {
								t.Errorf("eat sound = category %d at (%d,%d,%d), volume %v pitch %v", category, x, y, z, volume, pitch)
							}
						}
					}
					if id == protocol.IDPlayLevelParticles {
						want := int32(protocol.ParticleSmoke)
						if click == 0 {
							want, _, _ = registry.ParticleID("minecraft:effect")
						}
						if mooshroomParticle(t, data) == want {
							particles++
						}
					}
				})
				wantEats, wantParticles := 1, 4
				if click == 1 {
					wantEats, wantParticles = 0, 2
				}
				if eats != wantEats || particles != wantParticles {
					t.Errorf("click %d: eat sounds %d particles %d, want %d and %d", click, eats, particles, wantEats, wantParticles)
				}
				wantCount := int32(2)
				if mode == "creative" || click == 1 {
					wantCount = 3
				}
				if got := srv.InventorySlot("Florist", player.HotbarSlot); got.ItemID != flower || got.Count != wantCount {
					t.Fatalf("click %d: flower stack %+v, want count %d", click, got, wantCount)
				}
			}
			for bowl := range 2 {
				srv.GiveSlot("Florist", player.HotbarSlot, servertest.ItemStack(t, "minecraft:bowl", 1))
				c.Settle()
				c.UseOn(cow.EntityID)
				wantItem, wantSound := registry.ItemMushroomStew, "minecraft:entity.mooshroom.milk"
				if bowl == 0 {
					wantItem, wantSound = registry.ItemSuspiciousStew, "minecraft:entity.mooshroom.suspicious_milk"
				}
				found, sounds := false, 0
				c.SettleAndCollect(func(id int32, data []byte) {
					switch id {
					case protocol.IDPlayContainerSlot:
						r := protocol.NewReader(data)
						r.ReadVarInt()
						r.ReadVarInt()
						r.ReadI16()
						stack, ok := r.ReadSlot()
						if ok && stack.ItemID == int32(wantItem) {
							found = true
							if bowl == 0 {
								assertAlliumStew(t, stack)
							}
						}
					case protocol.IDPlaySoundEffect:
						name, _, _, _, _, volume, pitch := servertest.ReadAmbient(t, data)
						if name == wantSound {
							sounds++
							if volume != 1 || pitch != 1 {
								t.Errorf("milk volume %v pitch %v, want 1, 1", volume, pitch)
							}
						}
					}
				})
				if !found || sounds != 1 {
					t.Fatalf("bowl %d: stew found %v, milk sounds %d", bowl, found, sounds)
				}
			}
		})
	}
}

func assertAlliumStew(t *testing.T, stack protocol.ItemStack) {
	t.Helper()
	effects := stack.SuspiciousStewEffectsOf()
	if stack.ItemID != int32(registry.ItemSuspiciousStew) || len(effects) != 1 || effects[0].Effect != registry.MustMobEffect("minecraft:fire_resistance") || effects[0].Duration != 60 {
		t.Fatalf("stew %+v effects %+v, want fire resistance for 60 ticks", stack, effects)
	}
}
