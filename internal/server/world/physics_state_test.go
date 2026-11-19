package world

import (
	"testing"

	"github.com/google/uuid"

	"github.com/golem-mc/golem/registry"
)

func TestPhysicsStateWritesKeepOtherProperties(t *testing.T) {
	for _, tc := range []struct {
		name          string
		block         string
		before, after map[string]string
		act           func(*Physics)
	}{
		{"waterlogged trapdoor", "minecraft:oak_trapdoor",
			map[string]string{"facing": "west", "half": "top", "waterlogged": "true"},
			map[string]string{"facing": "west", "half": "top", "waterlogged": "true", "open": "true"},
			func(p *Physics) { p.UseBlock(8, GroundLevel, 8, "") }},
		{"gate turns away from opener", "minecraft:oak_fence_gate",
			map[string]string{"facing": "north", "in_wall": "true"},
			map[string]string{"facing": "south", "in_wall": "true", "open": "true"},
			func(p *Physics) { p.UseBlock(8, GroundLevel, 8, "south") }},
		{"locked repeater wraps delay", "minecraft:repeater",
			map[string]string{"facing": "east", "delay": "4", "locked": "true"},
			map[string]string{"facing": "east", "delay": "1", "locked": "true"},
			func(p *Physics) { p.UseBlock(8, GroundLevel, 8, "") }},
		{"waterlogged dripleaf hit", "minecraft:big_dripleaf",
			map[string]string{"facing": "west", "waterlogged": "true"},
			map[string]string{"facing": "west", "waterlogged": "true", "tilt": "full"},
			func(p *Physics) { p.DripleafProjectiled(8, GroundLevel, 8) }},
		{"summoning waterlogged shrieker", "minecraft:sculk_shrieker",
			map[string]string{"can_summon": "true", "waterlogged": "true"},
			map[string]string{"can_summon": "true", "waterlogged": "true", "shrieking": "true"},
			func(p *Physics) { p.StartShriek(8, GroundLevel, 8, 3) }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			w, p := newPhysicsWorld(t)
			w.SetBlock(8, GroundLevel, 8, registry.MustState(tc.block, tc.before))
			tc.act(p)
			if got, want := w.BlockAt(8, GroundLevel, 8), registry.MustState(tc.block, tc.after); got != want {
				t.Fatalf("state = %v, want %v", registry.StateProps(got), registry.StateProps(want))
			}
		})
	}
}

func TestHugeMushroomNeighbourClearsOnlyItsFace(t *testing.T) {
	w, p := newPhysicsWorld(t)
	const x, y, z = int32(8), GroundLevel, int32(8)
	for _, face := range []string{"down", "up", "north", "south", "west", "east"} {
		t.Run(face, func(t *testing.T) {
			for _, d := range dirDelta {
				w.SetBlock(x+d[0], y+d[1], z+d[2], airState)
			}
			block := registry.MustState("minecraft:brown_mushroom_block", nil)
			w.SetBlock(x, y, z, block)
			d := dirDelta[registry.FacingIndex(face)]
			nx, ny, nz := x+d[0], y+d[1], z+d[2]
			w.SetBlock(nx, ny, nz, block)
			p.NotifyChanged(nx, ny, nz, airState)
			want := registry.MustState("minecraft:brown_mushroom_block", map[string]string{face: "false"})
			if got := w.BlockAt(x, y, z); got != want {
				t.Fatalf("state = %v, want only %s cleared", registry.StateProps(got), face)
			}
		})
	}
}

func TestUnchangedLecternBookStillWakesNeighbours(t *testing.T) {
	w, p := newPhysicsWorld(t)
	lectern := registry.MustState("minecraft:lectern", map[string]string{"facing": "east"})
	w.SetBlock(8, GroundLevel, 8, lectern)
	w.SetBlock(9, GroundLevel, 8, registry.MustState("minecraft:redstone_wire", map[string]string{"power": "15"}))
	changes, _, _ := p.LecternBookChanged(8, GroundLevel, 8, false)
	if got := registry.StatePower(w.BlockAt(9, GroundLevel, 8)); got != 0 {
		t.Fatalf("wire power = %d, want 0", got)
	}
	if w.BlockAt(8, GroundLevel, 8) != lectern {
		t.Fatal("unchanged lectern was rewritten")
	}
	if len(changes) == 0 {
		t.Fatal("the lectern's neighbours were not updated")
	}
}

func TestWaterloggedSensorKeepsPropertiesThroughItsPhases(t *testing.T) {
	for _, name := range []string{"minecraft:sculk_sensor", "minecraft:calibrated_sculk_sensor"} {
		t.Run(name, func(t *testing.T) {
			w, p := newPhysicsWorld(t)
			props := map[string]string{"waterlogged": "true"}
			if name == "minecraft:calibrated_sculk_sensor" {
				props["facing"] = "west"
			}
			state := registry.MustState(name, props)
			w.SetBlock(8, GroundLevel, 8, state)
			w.SetBlock(9, GroundLevel, 8, registry.BlockAmethystBlock.DefaultState())
			source := uuid.New()
			p.Vibrate(Vibration{X: 8.5, Y: float64(GroundLevel) + 0.5, Z: 10.5, Frequency: 5, Player: source})
			clicks, resonances := 0, 0
			tick := int64(0)
			for _, phase := range []string{"active", "cooldown", "inactive"} {
				reached := false
				for range 40 {
					tick++
					_, _, _, effects := p.Tick(tick)
					for _, effect := range effects {
						if v := effect.ListenerEvent; v != nil {
							if v.Event == registry.GameEventResonate5 {
								resonances++
								if v.Player != source || v.Frequency != 5 || v.X != 9.5 || v.Y != float64(GroundLevel)+0.5 || v.Z != 8.5 {
									t.Fatalf("resonance lost its source, frequency or position: %+v", v)
								}
								continue
							}
							clicks++
							if v.Player != source || v.Event != registry.GameEventSculkSensorTendrilsClicking ||
								v.X != 8.5 || v.Y != float64(GroundLevel)+0.5 || v.Z != 8.5 {
								t.Fatalf("sensor click lost its source or position: %+v", v)
							}
						}
						if effect.SoundID == registry.SoundBlockSculkSensorClicking || effect.SoundID == registry.SoundBlockSculkSensorClickingStop {
							t.Fatal("waterlogged sensor played a click")
						}
					}
					got := w.BlockAt(8, GroundLevel, 8)
					if registry.StateValue(got, registry.PropSculkSensorPhase) != phase {
						continue
					}
					if !registry.StateBool(got, registry.PropWaterlogged) || (name == "minecraft:calibrated_sculk_sensor" && registry.StateValue(got, registry.PropFacing) != "west") {
						t.Fatalf("sensor lost properties: %v", registry.StateProps(got))
					}
					if (registry.StatePower(got) > 0) != (phase == "active") {
						t.Fatalf("phase %s has power %d", phase, registry.StatePower(got))
					}
					reached = true
					break
				}
				if !reached {
					t.Fatalf("sensor never reached %s", phase)
				}
				if clicks != 1 || resonances != 1 {
					t.Fatalf("sensor emitted %d clicks and %d resonances through phase %s, want 1 each", clicks, resonances, phase)
				}
			}
		})
	}
}
