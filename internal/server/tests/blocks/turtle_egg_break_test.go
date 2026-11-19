package blocks

import (
	"sync/atomic"
	"testing"

	"github.com/golem-mc/golem/internal/config"
	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/internal/server/servertest"
	"github.com/golem-mc/golem/registry"
)

// vanilla: TurtleEggBlock.decreaseEggs
func TestBreakingATurtleEggTakesOneEgg(t *testing.T) {
	t.Parallel()
	cfg := servertest.SurvivalConfig()
	cfg.ViewDistance, cfg.SimulationDistance = config.MinDistance, config.MinDistance
	srv, c := servertest.Join(t, cfg, "Digger")
	c.MoveToGround(0.5, 64, 0.5, true)
	c.Settle()
	servertest.HoldTicks(t, srv)
	d := servertest.DigTarget
	srv.SetBlock(d[0], d[1]-1, d[2], registry.BlockSand.DefaultState())
	for _, tc := range []struct {
		name     string
		eggs     string
		creative bool
		want     string
	}{
		{name: "three", eggs: "3", want: "2"},
		{name: "last", eggs: "1", want: ""},
		{name: "creative", eggs: "3", creative: true, want: ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			mode := int32(protocol.GameModeSurvival)
			if tc.creative {
				mode = int32(protocol.GameModeCreative)
			}
			srv.SetPlayerGameMode("Digger", mode)
			state := registry.MustState("minecraft:turtle_egg", map[string]string{"eggs": tc.eggs, "hatch": "1"})
			srv.SetBlock(d[0], d[1], d[2], state)
			c.Settle()
			var destroys atomic.Int32
			var eventState atomic.Uint32
			srv.ObserveGameEvents(func(event string, x, y, z float64) {
				if event == "minecraft:block_destroy" && x == float64(d[0])+0.5 &&
					y == float64(d[1])+0.5 && z == float64(d[2])+0.5 {
					destroys.Add(1)
					eventState.Store(uint32(srv.BlockAt(d[0], d[1], d[2])))
				}
			})
			c.BreakBlock(d[0], d[1], d[2])
			c.SettleKeeping()
			want := registry.BlockAir.DefaultState()
			if tc.want != "" {
				want = registry.MustState("minecraft:turtle_egg", map[string]string{"eggs": tc.want, "hatch": "1"})
			}
			servertest.UntilTicks(t, srv, 40, "the egg to break", func() bool {
				return srv.BlockAt(d[0], d[1], d[2]) == want
			})
			var sounds, breaks int
			var updated bool
			c.ReadTicks(srv, 1, func(id int32, data []byte) bool {
				switch id {
				case protocol.IDPlaySoundEffect:
					name, category, _, _, _, volume, pitch := servertest.ReadAmbient(t, data)
					if name == "minecraft:entity.turtle.egg_break" {
						sounds++
						if category != protocol.SoundCategoryBlock || volume != 0.7 || pitch < 0.9 || pitch > 1.1 {
							t.Errorf("egg sound category/volume/pitch = %d/%v/%v", category, volume, pitch)
						}
					}
				case protocol.IDPlayWorldEvent:
					r := protocol.NewReader(data)
					if r.ReadI32() == protocol.WorldEventBlockBreak {
						x, y, z := r.ReadPosition()
						if x == d[0] && y == d[1] && z == d[2] {
							breaks++
							if got := uint16(r.ReadI32()); got != state {
								t.Errorf("break event state = %d, want %d", got, state)
							}
						}
					}
				case protocol.IDPlayBlockUpdate, protocol.IDPlaySectionBlocks:
					for _, ch := range servertest.BlockChangesIn(t, id, data) {
						if ch.X == d[0] && ch.Y == d[1] && ch.Z == d[2] && ch.State == want {
							updated = true
						}
					}
				}
				return false
			})
			wantSounds, wantBreaks, wantDestroys := 1, 0, int32(1)
			if tc.creative {
				wantSounds = 0
			} else if tc.want != "" {
				wantBreaks, wantDestroys = 1, 2
				if got := eventState.Load(); got != uint32(want) {
					t.Errorf("state at the extra destroy event = %d, want %d", got, want)
				}
			}
			if sounds != wantSounds || breaks != wantBreaks || destroys.Load() != wantDestroys || !updated {
				t.Errorf("sounds/break events/destroy events/update = %d/%d/%d/%v, want %d/%d/%d/true",
					sounds, breaks, destroys.Load(), updated, wantSounds, wantBreaks, wantDestroys)
			}
		})
	}
}
