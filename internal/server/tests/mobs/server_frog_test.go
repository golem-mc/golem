package mobs

import (
	"fmt"
	"math"
	"testing"

	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/internal/server/servertest"
	"github.com/golem-mc/golem/nbt"
	"github.com/golem-mc/golem/registry"
)

func TestFrogTongueAndDrops(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name, prey, variant, drop string
		size                      int
	}{
		{"cold", "minecraft:magma_cube", "minecraft:cold", "minecraft:verdant_froglight", 1},
		{"warm", "minecraft:magma_cube", "minecraft:warm", "minecraft:pearlescent_froglight", 1},
		{"temperate", "minecraft:magma_cube", "minecraft:temperate", "minecraft:ochre_froglight", 1},
		{"slime", "minecraft:slime", "minecraft:cold", "minecraft:slime_ball", 1},
		{"large", "minecraft:slime", "minecraft:cold", "", 2},
	} {
		t.Run(tc.name, func(t *testing.T) {
			srv, c := servertest.BreedingPlayer(t, "creative", "Frogger", "", 0)
			servertest.HoldTicks(t, srv)
			frog := srv.SpawnMob("minecraft:frog", 8.5, 64, 0.5)
			c.ExpectMobSpawn(fishType(t, "minecraft:frog"))
			srv.SetMobVariant(frog, tc.variant)
			prey := srv.SpawnMob(tc.prey, 9.5, 64, 0.5)
			srv.SetMobSize(prey, tc.size)
			srv.SetMobNoAI(prey)
			c.ExpectMobSpawn(fishType(t, tc.prey))
			posed, targeted, removed, cleared := false, false, false, false
			tongueSound, eatSound := false, false
			c.ReadTicks(srv, 60, func(id int32, data []byte) bool {
				r := protocol.NewReader(data)
				switch id {
				case protocol.IDPlayEntityMetadata:
					if r.ReadVarInt() != frog {
						return false
					}
					for r.Err() == nil {
						index := r.ReadU8()
						if index == protocol.MetadataEnd {
							break
						}
						typ := r.ReadVarInt()
						switch index {
						case protocol.MetadataIndexPose:
							pose := r.ReadVarInt()
							posed = posed || pose == protocol.PoseUsingTongue
						case protocol.MetadataIndexFrogTongueTarget:
							if typ != protocol.MetadataOptionalVarInt {
								t.Fatalf("tongue serializer=%d", typ)
							}
							target := r.ReadVarInt()
							targeted = targeted || target == prey+1
							cleared = cleared || targeted && target == 0
						default:
							if typ == protocol.MetadataFrogVariant {
								r.ReadVarInt()
							} else {
								servertest.SkipMetadataField(c, r, typ)
							}
						}
					}
					if r.Err() != nil {
						t.Fatal(r.Err())
					}
				case protocol.IDPlaySoundEffect:
					name, category, _, _, _, volume, pitch := servertest.ReadAmbient(t, data)
					if name == "minecraft:entity.frog.tongue" || name == "minecraft:entity.frog.eat" {
						if category != protocol.SoundCategoryNeutral || volume != 2 || pitch != 1 {
							t.Fatalf("frog sound category=%d volume=%v pitch=%v", category, volume, pitch)
						}
						tongueSound = tongueSound || name == "minecraft:entity.frog.tongue"
						eatSound = eatSound || name == "minecraft:entity.frog.eat"
					}
				case protocol.IDPlayRemoveEntities:
					for range r.ReadVarInt() {
						removed = r.ReadVarInt() == prey || removed
					}
				}
				return false
			})
			if tc.drop == "" {
				if posed || targeted || removed {
					t.Fatalf("large slime: pose=%v target=%v removed=%v", posed, targeted, removed)
				}
				if _, ok := srv.MobHealth(prey); !ok {
					t.Fatal("large slime disappeared")
				}
			} else {
				if !tongueSound || !eatSound {
					t.Fatal("missing tongue or eat sound")
				}
				if !posed || !targeted || !removed || !cleared {
					t.Fatalf("pose=%v target=%v removed=%v cleared=%v", posed, targeted, removed, cleared)
				}
				if _, ok := srv.MobHealth(prey); ok {
					t.Fatal("eaten prey remains")
				}
				items := srv.ItemsInDimension("minecraft:overworld")
				if len(items) != 1 || items[0].Item != tc.drop || items[0].Count != 1 {
					t.Fatalf("drops=%+v, want one %s", items, tc.drop)
				}
			}
		})
	}
}

func TestTwoFedFrogsLayFrogspawn(t *testing.T) {
	t.Parallel()
	srv, c := servertest.BreedingPlayer(t, "survival", "Frogger", "minecraft:slime_ball", 2, servertest.OpsListing(t, "Frogger"))
	servertest.SetFarmBlock(t, c, srv, 3, 63, 2, "minecraft:water", nil)
	servertest.HoldTicks(t, srv)
	first := srv.SpawnMob("minecraft:frog", 1.5, 64, 2.5)
	second := srv.SpawnMob("minecraft:frog", 2.5, 64, 2.5)
	c.ExpectMobSpawn(fishType(t, "minecraft:frog"))
	c.ExpectMobSpawn(fishType(t, "minecraft:frog"))
	c.UseOn(first)
	c.AwaitEntityEvent(first, loveHearts)
	c.UseOn(second)
	c.AwaitEntityEvent(second, loveHearts)
	sound := false
	c.ReadTicks(srv, 300, func(id int32, data []byte) bool {
		if id == protocol.IDPlaySoundEffect {
			name, category, _, _, _, volume, pitch := servertest.ReadAmbient(t, data)
			if name == "minecraft:entity.frog.lay_spawn" {
				sound = true
				if category != protocol.SoundCategoryBlock || volume != 1 || pitch != 1 {
					t.Errorf("lay sound: category=%d volume=%v pitch=%v", category, volume, pitch)
				}
			}
		}
		return false
	})
	if got := srv.BlockAt(3, 64, 2); got != registry.BlockFrogspawn.DefaultState() {
		t.Errorf("block over water=%d, want frogspawn", got)
	}
	if !sound {
		t.Error("missing lay_spawn sound")
	}
	if srv.MobCount() != 2 {
		t.Errorf("mobs=%d, want only two parents", srv.MobCount())
	}
	for _, m := range srv.Mobs() {
		if m.Baby || m.InLove != 0 || m.Age < 5700 || m.Age > 6000 {
			t.Errorf("parent=%+v", m)
		}
	}
}

// frogWatch records the poses a frog's Set Entity Metadata carries and
// the sounds played, per sound name.
func frogWatch(t *testing.T, c *servertest.Client, frog int32, poses map[int32]bool, sounds map[string]int) func(int32, []byte) bool {
	return func(id int32, data []byte) bool {
		switch id {
		case protocol.IDPlayEntityMetadata:
			if pose, ok := goatPoses(c, frog, data); ok {
				poses[pose] = true
			}
		case protocol.IDPlaySoundEffect:
			name, _, _, _, _, _, _ := servertest.ReadAmbient(t, data)
			sounds[name]++
		}
		return false
	}
}

// TestAFrogLongJumpsToALilyPad: on a stone platform in the sky, a frog whose
// cooldown is gone has a lily pad on dry ground and one over water to choose
// from; it long jumps to the dry one, in the long jumping pose, with the
// long_jump sound and then the step sound.
// vanilla: FrogAi.isAcceptableLandingSpot, LongJumpToPreferredBlock
func TestAFrogLongJumpsToALilyPad(t *testing.T) {
	t.Parallel()
	srv, c := servertest.BreedingPlayer(t, "survival", goatOp, "", 0, servertest.OpsListing(t, goatOp))
	servertest.HoldTicks(t, srv)
	c.ChatCommand("fill 0 98 0 2 98 2 minecraft:stone")
	c.ChatCommand("fill 0 99 0 2 99 2 minecraft:stone")
	c.ChatCommand("setblock 5 99 1 minecraft:stone")
	c.ChatCommand("setblock 5 100 1 minecraft:lily_pad")
	c.ChatCommand("fill -6 97 0 -4 97 2 minecraft:stone")
	c.ChatCommand("fill -6 98 0 -4 99 2 minecraft:water")
	c.ChatCommand("setblock -5 100 1 minecraft:lily_pad")
	c.ChatCommand("tp @s 0.5 100 0.5")
	c.ReadTicks(srv, 2, func(int32, []byte) bool { return false })
	id := srv.SpawnMobSeeded("minecraft:frog", 1.5, 100, 1.5, 7)
	c.ExpectMobSpawn(fishType(t, "minecraft:frog"))
	srv.SetFrogLongJumpCooldown(id, 0)
	poses, sounds := map[int32]bool{}, map[string]int{}
	watch := frogWatch(t, c, id, poses, sounds)
	c.ReadTicks(srv, 600, func(packetID int32, data []byte) bool {
		watch(packetID, data)
		return sounds["minecraft:entity.frog.step"] > 0
	})
	if sounds["minecraft:entity.frog.long_jump"] == 0 || sounds["minecraft:entity.frog.step"] == 0 {
		t.Fatalf("sounds %v, want long_jump then step", sounds)
	}
	if !poses[protocol.PoseLongJumping] {
		t.Fatalf("poses seen %v, want the long jumping pose", poses)
	}
	x, y, z, _ := srv.MobPos(id)
	if x < 5 || x > 6 || y < 100 || y > 100.2 {
		t.Errorf("the frog is at %.2f %.2f %.2f, want it on the lily pad", x, y, z)
	}
}

// TestAFrogLongJumpsToATrapdoor: across a gap a frog lands on a closed
// trapdoor, which is not a preferred block but has the TRAPDOOR path type.
// vanilla: FrogAi.isAcceptableLandingSpot
func TestAFrogLongJumpsToATrapdoor(t *testing.T) {
	t.Parallel()
	srv, c := servertest.BreedingPlayer(t, "survival", goatOp, "", 0, servertest.OpsListing(t, goatOp))
	servertest.HoldTicks(t, srv)
	c.ChatCommand("fill 0 98 0 2 98 2 minecraft:stone")
	c.ChatCommand("fill 0 99 0 2 99 2 minecraft:stone")
	c.ChatCommand("setblock 5 98 1 minecraft:stone")
	c.ChatCommand("setblock 5 99 1 minecraft:oak_trapdoor")
	c.ChatCommand("tp @s 0.5 100 0.5")
	c.ReadTicks(srv, 2, func(int32, []byte) bool { return false })
	id := srv.SpawnMobSeeded("minecraft:frog", 1.5, 100, 1.5, 7)
	c.ExpectMobSpawn(fishType(t, "minecraft:frog"))
	srv.SetFrogLongJumpCooldown(id, 0)
	poses, sounds := map[int32]bool{}, map[string]int{}
	watch := frogWatch(t, c, id, poses, sounds)
	c.ReadTicks(srv, 600, func(packetID int32, data []byte) bool {
		watch(packetID, data)
		return sounds["minecraft:entity.frog.step"] > 0
	})
	if sounds["minecraft:entity.frog.long_jump"] == 0 {
		t.Fatalf("sounds %v, want a long_jump", sounds)
	}
	x, y, z, _ := srv.MobPos(id)
	if x < 4.9 || x > 6 || y < 99 || y > 99.4 {
		t.Errorf("the frog is at %.2f %.2f %.2f, want it on the trapdoor", x, y, z)
	}
}

// TestAnIdleFrogCroaks: a frog with nothing to do shows the croaking pose.
// vanilla: Croak
func TestAnIdleFrogCroaks(t *testing.T) {
	t.Parallel()
	srv, c := servertest.BreedingPlayer(t, "survival", goatOp, "", 0, servertest.OpsListing(t, goatOp))
	servertest.HoldTicks(t, srv)
	c.ChatCommand("fill 0 99 0 4 99 4 minecraft:stone")
	c.ChatCommand("tp @s 0.5 100 0.5")
	c.ReadTicks(srv, 2, func(int32, []byte) bool { return false })
	id := srv.SpawnMobSeeded("minecraft:frog", 2.5, 100, 2.5, 5)
	c.ExpectMobSpawn(fishType(t, "minecraft:frog"))
	poses := map[int32]bool{}
	c.ReadTicks(srv, 200, func(packetID int32, data []byte) bool {
		if packetID == protocol.IDPlayEntityMetadata {
			if pose, ok := goatPoses(c, id, data); ok {
				poses[pose] = true
			}
		}
		return poses[protocol.PoseCroaking]
	})
	if !poses[protocol.PoseCroaking] {
		t.Fatalf("poses seen %v, want the croaking pose", poses)
	}
}

// TestAPregnantFrogKeepsItsSpawnAcrossSave: a frog summoned with vanilla's
// saved IS_PREGNANT brain memory beside water lays frogspawn, and a frog
// bred away from water shows the memory under Brain.
func TestAPregnantFrogKeepsItsSpawnAcrossSave(t *testing.T) {
	t.Run("summoned", func(t *testing.T) {
		t.Parallel()
		srv, c := servertest.BreedingPlayer(t, "creative", "Frogload", "", 0, servertest.OpsListing(t, "Frogload"))
		servertest.SetFarmBlock(t, c, srv, 3, 63, 2, "minecraft:water", nil)
		c.ChatCommand(`summon minecraft:frog 1.5 64 2.5 {Brain:{memories:{"minecraft:is_pregnant":{value:{}}}}}`)
		c.ExpectMobSpawn(fishType(t, "minecraft:frog"))
		frogspawn := registry.BlockFrogspawn.DefaultState()
		servertest.UntilTicks(t, srv, 300, "frogspawn over the water", func() bool {
			return srv.BlockAt(3, 64, 2) == frogspawn
		})
	})
	t.Run("bred", func(t *testing.T) {
		t.Parallel()
		srv, c := servertest.BreedingPlayer(t, "survival", "Frogbred", "minecraft:slime_ball", 2, servertest.OpsListing(t, "Frogbred"))
		first := srv.SpawnMob("minecraft:frog", 1.5, 64, 2.5)
		second := srv.SpawnMob("minecraft:frog", 2.5, 64, 2.5)
		c.ExpectMobSpawn(fishType(t, "minecraft:frog"))
		c.ExpectMobSpawn(fishType(t, "minecraft:frog"))
		c.UseOn(first)
		c.AwaitEntityEvent(first, loveHearts)
		c.UseOn(second)
		c.AwaitEntityEvent(second, loveHearts)
		servertest.UntilTicks(t, srv, 300, "the frogs to breed", func() bool {
			for _, m := range srv.Mobs() {
				if m.InLove != 0 {
					return false
				}
			}
			return true
		})
		pregnant := 0
		for _, id := range []int32{first, second} {
			id, _ := srv.MobUUID(id)
			brain, _ := nbt.Get[nbt.Compound](entityCompound(t, c, id), "Brain")
			memories, _ := nbt.Get[nbt.Compound](brain, "memories")
			if _, ok := nbt.Get[nbt.Compound](memories, "minecraft:is_pregnant"); ok {
				pregnant++
			}
		}
		if pregnant != 1 {
			t.Errorf("frogs with the is_pregnant memory = %d, want 1", pregnant)
		}
	})
}

// TestAFrogHoversInWaterInsteadOfSinking: a frog with no movement speed
// half-way down a 5-deep water column never sinks: its water travel has no
// gravity, and its move control lifts it 0.005 a tick, so it floats up. A
// NoAI frog would prove nothing, as no NoAI mob travels at all.
// vanilla: Frog.travelInWater, SmoothSwimmingMoveControl's applyGravity,
// LivingEntity.aiStep
func TestAFrogHoversInWaterInsteadOfSinking(t *testing.T) {
	t.Parallel()
	srv, c := servertest.BreedingPlayer(t, "survival", goatOp, "", 0, servertest.OpsListing(t, goatOp))
	servertest.HoldTicks(t, srv)
	c.ChatCommand("fill 4 59 4 4 63 4 minecraft:water")
	c.ChatCommand(`summon minecraft:frog 4.5 61 4.5 {attributes:[{id:"minecraft:movement_speed",base:0.0d}]}`)
	c.ExpectMobSpawn(fishType(t, "minecraft:frog"))
	id := srv.MobIDsOfKind("minecraft:frog")[0]
	_, y0, _, _ := srv.MobPos(id)
	low := y0
	for range 100 {
		servertest.RunTicks(t, srv, 1)
		_, y, _, _ := srv.MobPos(id)
		low = min(low, y)
	}
	_, y, _, _ := srv.MobPos(id)
	if low < y0-0.1 || y < y0+1 {
		t.Errorf("the frog went from y %.3f down to %.3f and ended at %.3f, want it never below %.3f and floated up", y0, low, y, y0-0.1)
	}
}

// TestAFrogInAPoolSwimsToLand: a frog on the floor of a 3-deep pool walled
// in stone eventually finds the stone rim and climbs out onto it. Random
// swim targets and retries can keep even a vanilla frog in the pool past
// 200 ticks; allow several attempts across the seeded paths.
// vanilla: FrogAi.initSwimActivity, TryFindLand, PathNavigation.shouldTargetNextNodeInDirection
func TestAFrogInAPoolSwimsToLand(t *testing.T) {
	t.Parallel()
	for seed := int64(1); seed <= 8; seed++ {
		t.Run(fmt.Sprintf("seed%d", seed), func(t *testing.T) {
			t.Parallel()
			srv, c := servertest.BreedingPlayer(t, "survival", goatOp, "", 0, servertest.OpsListing(t, goatOp))
			servertest.HoldTicks(t, srv)
			c.ChatCommand("fill 2 60 2 8 63 8 minecraft:stone")
			c.ChatCommand("fill 3 61 3 7 63 7 minecraft:water")
			c.ReadTicks(srv, 2, func(int32, []byte) bool { return false })
			id := srv.SpawnMobSeeded("minecraft:frog", 5.5, 61, 5.5, seed)
			servertest.UntilTicks(t, srv, 1000, "the frog standing on land", func() bool {
				x, y, z, _ := srv.MobPos(id)
				feet := srv.BlockAt(int32(math.Floor(x)), int32(math.Floor(y)), int32(math.Floor(z)))
				return srv.MobOnGround(id) && registry.BlockOf(feet) != registry.BlockWater
			})
		})
	}
}
