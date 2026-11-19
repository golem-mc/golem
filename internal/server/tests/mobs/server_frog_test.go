package mobs

import (
	"testing"

	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/internal/server/servertest"
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
