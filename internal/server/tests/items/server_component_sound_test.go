package items

import (
	"fmt"
	"math"
	"slices"
	"testing"

	"github.com/golem-mc/golem/internal/config"
	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/internal/server/servertest"
	"github.com/golem-mc/golem/registry"
)

// vanilla: Consumable.onConsume, PlaySoundConsumeEffect.apply
func TestConsumeEffectsKeepOrderAroundTeleport(t *testing.T) {
	t.Parallel()
	for _, x := range []float64{1.125, -1.125} {
		t.Run(fmt.Sprint(x), func(t *testing.T) {
			t.Parallel()
			srv, w := givenWindowed(t, servertest.SurvivalTestConfig(),
				`stick[consumable={consume_seconds:0,on_consume_effects:[`+
					`{type:"play_sound",sound:"minecraft:block.amethyst_block.chime"},`+
					`{type:"apply_effects",effects:[{id:"minecraft:speed",duration:200}],probability:1},`+
					`{type:"teleport_randomly",diameter:1},`+
					`{type:"play_sound",sound:"minecraft:block.note_block.bell"},`+
					`{type:"remove_effects",effects:"minecraft:speed"}]}]`)
			// Every candidate is above the flat floor, so the teleport succeeds
			// and drops to a different block regardless of its random draws.
			w.MoveToGround(x, 70.25, x*2, false)
			w.Settle()
			before := srv.PlayerPosition("Testificate")
			w.UseItemRightClicks(protocol.HandMain)
			w.Settle()
			servertest.RunTicks(t, srv, 1)
			after := srv.PlayerPosition("Testificate")
			centre := func(x, y, z float64) [3]int32 {
				return [3]int32{int32(math.Floor(x)*8 + 4), int32(math.Floor(y)*8 + 4), int32(math.Floor(z)*8 + 4)}
			}
			pre, post := centre(before.X, before.Y, before.Z), centre(after.X, after.Y, after.Z)
			if pre == post {
				t.Fatalf("teleport did not change blocks: %v -> %v", before, after)
			}
			var order []string
			w.SettleAndCollect(func(id int32, data []byte) {
				switch id {
				case protocol.IDPlaySoundEffect:
					sound := servertest.DecodePlaySoundEffect(t, data)
					var want [3]int32
					switch sound.RegistryID + 1 {
					case int32(registry.SoundBlockAmethystBlockChime):
						order = append(order, "before sound")
						want = pre
					case int32(registry.SoundBlockNoteBlockBell):
						order = append(order, "after sound")
						want = post
					default:
						return
					}
					if got := [3]int32{sound.X, sound.Y, sound.Z}; got != want {
						t.Errorf("%s position = %v, want block centre %v", order[len(order)-1], got, want)
					}
				case protocol.IDPlayEntityEvent:
					r := protocol.NewReader(data)
					if r.ReadI32() == w.PlayerEntityID && r.ReadU8() == protocol.EntityStatusTeleportParticles {
						order = append(order, "teleport")
					}
				case protocol.IDPlayUpdateMobEffect:
					order = append(order, "apply effect")
				case protocol.IDPlayRemoveMobEffect:
					order = append(order, "remove effect")
				}
			})
			want := []string{"before sound", "apply effect", "teleport", "after sound", "remove effect"}
			if !slices.Equal(order, want) {
				t.Errorf("consume effect order = %v, want %v", order, want)
			}
		})
	}
}

// vanilla: PlaySoundConsumeEffect.apply, Level.playSound(BlockPos)
func TestConsumeEffectSoundUsesBlockCentre(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name    string
		x, y, z float64
		want    [3]int32
	}{
		{"positive", 1.125, 65.25, 2.875, [3]int32{12, 524, 20}},
		{"negative", -1.125, 65.875, -2.25, [3]int32{-12, 524, -20}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			srv, w := givenWindowed(t, servertest.SurvivalTestConfig(),
				`stick[consumable={consume_seconds:0,on_consume_effects:[{type:"play_sound",sound:"minecraft:block.amethyst_block.chime"}]}]`)
			w.MoveToGround(tc.x, tc.y, tc.z, false)
			w.Settle()
			w.UseItemRightClicks(protocol.HandMain)
			w.Settle()
			servertest.RunTicks(t, srv, 1)
			count := 0
			w.SettleAndCollect(func(id int32, data []byte) {
				if id != protocol.IDPlaySoundEffect {
					return
				}
				sound := servertest.DecodePlaySoundEffect(t, data)
				if sound.RegistryID != int32(registry.SoundBlockAmethystBlockChime)-1 {
					return
				}
				count++
				// Sound packet coordinates are in eighths of a block.
				if got := [3]int32{sound.X, sound.Y, sound.Z}; got != tc.want {
					t.Errorf("consume effect sound position = %v, want block centre %v", got, tc.want)
				}
			})
			if count != 1 {
				t.Fatalf("heard %d consume effect sounds, want 1", count)
			}
		})
	}
}

// vanilla: SoundEvent.DIRECT_STREAM_CODEC, LivingEntity.onEquipItem,
// Consumable.emitParticlesAndSounds, PlaySoundConsumeEffect.apply
func TestComponentSoundHoldersReachPlayback(t *testing.T) {
	t.Parallel()
	for _, action := range []string{"consume", "consume effect", "equip"} {
		for _, form := range []string{"inline known", "inline unknown", "registered"} {
			t.Run(action+"/"+form, func(t *testing.T) {
				t.Parallel()
				name := "minecraft:block.amethyst_block.chime"
				if form == "inline unknown" {
					name = "test:component_sound"
				}
				sound := fmt.Sprintf(`{sound_id:%q,range:32}`, name)
				if form == "registered" {
					sound = fmt.Sprintf(`%q`, name)
				}
				var item string
				switch action {
				case "consume":
					item = `stick[consumable={consume_seconds:0,sound:` + sound + `},food={nutrition:1,saturation:0,can_always_eat:true}]`
				case "consume effect":
					item = `stick[consumable={consume_seconds:0,on_consume_effects:[{type:"play_sound",sound:` + sound + `}]}]`
				case "equip":
					item = `stick[equippable={slot:"head",equip_sound:` + sound + `}]`
				}
				srv, w := givenWindowed(t, servertest.SurvivalTestConfig(), item)
				w.UseItemRightClicks(protocol.HandMain)
				if action != "equip" {
					w.Settle()
					servertest.RunTicks(t, srv, 1)
				}
				found := false
				w.SettleAndCollect(func(id int32, data []byte) {
					if id != protocol.IDPlaySoundEffect {
						return
					}
					packet := servertest.DecodePlaySoundEffect(t, data)
					wireID := packet.RegistryID + 1
					got := packet.Name
					if wireID != 0 {
						got, _ = servertest.SoundEventName(packet.RegistryID)
					}
					if got != name {
						return
					}
					found = true
					if form == "registered" {
						want, _ := registry.SoundEventID(name)
						if wireID != want+1 {
							t.Errorf("sound holder ID = %d, want %d", wireID, want+1)
						}
					} else if wireID != 0 || !packet.HasFixedRange || packet.FixedRange != 32 {
						t.Errorf("sound holder = (ID %d, range %v/%v), want inline range 32", wireID, packet.HasFixedRange, packet.FixedRange)
					}
				})
				if !found {
					t.Fatalf("no playback of %s", name)
				}
			})
		}
	}
}

// vanilla: ServerLevel.playSeededSound, SoundEvent.getRange
func TestComponentFixedSoundRangeControlsEarshot(t *testing.T) {
	t.Parallel()
	for _, action := range []string{"equip", "consume"} {
		for _, fixedRange := range []int{8, 32} {
			t.Run(fmt.Sprintf("%s/range%d", action, fixedRange), func(t *testing.T) {
				t.Parallel()
				cfg := servertest.SurvivalTestConfig()
				cfg.ViewDistance, cfg.SimulationDistance = config.MinDistance, config.MinDistance
				cfg.LevelName = t.TempDir()
				srv, connect := servertest.Start(t, cfg)
				servertest.FreezeUntilHeld(t, srv)
				actor := connect().Join("Actor")
				actor.MoveToGround(0.5, 64, 0.5, true)
				witness := connect().Join("Witness")
				witness.MoveToGround(24.5, 64, 0.5, true)
				servertest.HoldTicks(t, srv)
				sound := fmt.Sprintf(`{sound_id:"minecraft:block.amethyst_block.chime",range:%d}`, fixedRange)
				item := `stick[equippable={slot:"head",equip_sound:` + sound + `}]`
				if action == "consume" {
					item = `stick[consumable={consume_seconds:0.25,sound:` + sound + `}]`
				}
				srv.RunLine("give Actor " + item)
				actor.Settle()
				witness.Settle()
				actor.UseItemRightClicks(protocol.HandMain)
				actor.Settle()
				if action == "consume" {
					servertest.RunTicks(t, srv, 5)
				}
				count := 0
				witness.SettleAndCollect(func(id int32, data []byte) {
					if id != protocol.IDPlaySoundEffect {
						return
					}
					sound := servertest.DecodePlaySoundEffect(t, data)
					if sound.Name == "minecraft:block.amethyst_block.chime" {
						count++
						if !sound.HasFixedRange || sound.FixedRange != float32(fixedRange) {
							t.Errorf("sound range = %v/%v, want %d", sound.HasFixedRange, sound.FixedRange, fixedRange)
						}
					}
				})
				if (count > 0) != (fixedRange == 32) {
					t.Errorf("witness at 24 blocks heard %d sounds with fixed range %d", count, fixedRange)
				}
			})
		}
	}
}
