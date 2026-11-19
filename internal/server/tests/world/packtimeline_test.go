package world

import (
	"testing"

	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/internal/server/servertest"
	"github.com/golem-mc/golem/registry"
)

// vanilla: EnvironmentAttributeSystem.addDynamicLayers, Mob.isSunBurnTick,
// DaylightDetectorBlock.updateSignalStrength
func TestPackDimensionTypesEvaluateTimelines(t *testing.T) {
	// Pack registries are process-wide; finish before the parallel tests start.
	t.Cleanup(func() { registry.ApplyPackRegistries(nil); registry.ApplyPackTags(nil) })
	cfg := servertest.BlockTestConfig()
	cfg.LevelName = t.TempDir()
	files := map[string]string{
		"data/pk/tags/timeline/day.json": `{"values":["minecraft:day"]}`,
		"data/pk/timeline/shift.json": `{"clock":"minecraft:overworld","period_ticks":24000,"tracks":{
			"minecraft:gameplay/monsters_burn":{"modifier":"or","keyframes":[{"ticks":0,"value":false},{"ticks":12000,"value":true}]},
			"minecraft:gameplay/sky_light_level":{"ease":"constant","keyframes":[{"ticks":0,"value":4},{"ticks":12000,"value":15}]},
			"minecraft:visual/sun_angle":{"ease":"constant","keyframes":[{"ticks":0,"value":180},{"ticks":12000,"value":0}]}}}`,
	}
	for name, timelines := range map[string]string{"day": `"#pk:day"`, "still": `[]`, "shift": `["pk:shift"]`} {
		files["data/pk/dimension_type/"+name+".json"] = `{"min_y":-64,"height":384,"logical_height":384,"has_skylight":true,"timelines":` + timelines + `}`
		files["data/pk/dimension/"+name+".json"] = `{"type":"pk:` + name + `","generator":{"type":"minecraft:flat",
			"settings":{"biome":"minecraft:plains","layers":[{"block":"minecraft:stone","height":128}]}}}`
	}
	writePack(t, cfg.LevelName, files)
	srv, c := servertest.Join(t, cfg, "Voyager", servertest.OpsListing(t, "Voyager"))
	servertest.HoldTicks(t, srv)
	srv.SetPaused(true)
	for _, name := range []string{"day", "still", "shift"} {
		dim := "pk:" + name
		c.ChatCommand("execute in " + dim + " run tp @s 0 64 0")
		c.ExpectWithinTicks(srv, 200, protocol.IDPlayRespawn)
		// The chunk loads on workers beside the held loop, so wait on the
		// senders, not on ticks.
		servertest.AwaitChunksSettled(t, srv)
		servertest.UntilTicks(t, srv, 200, "the destination chunk in "+dim, func() bool {
			return srv.ChunkLoadedInDimension(dim, 6, 6)
		})
		c.ChatCommand("setblock 6 64 6 daylight_detector")
		c.Settle()
		servertest.UntilTicks(t, srv, 200, "the detector in "+dim, func() bool {
			return registry.BlockOf(srv.BlockInDimension(dim, 6, 64, 6)) == registry.BlockDaylightDetector
		})
		for _, tc := range []struct {
			time                 int64
			dayPower, shiftPower int
			dayBurn, shiftBurn   bool
		}{{6000, 15, 0, true, false}, {0, 7, 0, true, false}, {18000, 0, 15, false, true}} {
			srv.SetDayTime(tc.time)
			mob := srv.SpawnMobInDimension("minecraft:zombie", dim, 3.5, 64, 3.5)
			burnt := false
			// Hold the clock while sunlight's random ignition and the detector's
			// twenty-tick cadence run. Read the fire flag sent to the watcher.
			c.ReadTicks(srv, 120, func(id int32, data []byte) bool {
				if id == protocol.IDPlayEntityMetadata && protocol.NewReader(data).ReadVarInt() == mob {
					m := servertest.ReadZombieMetadata(c, data)
					burnt = burnt || m.EntityID == mob && m.HasFire && m.OnFire
				}
				return false
			})
			power, burn := 15, false
			if name == "day" {
				power, burn = tc.dayPower, tc.dayBurn
			}
			if name == "shift" {
				power, burn = tc.shiftPower, tc.shiftBurn
			}
			if got := registry.StatePower(srv.BlockInDimension(dim, 6, 64, 6)); got != power {
				t.Errorf("%s time %d: power = %d, want %d", dim, tc.time, got, power)
			}
			dark := 0
			if name == "day" && tc.time == 18000 || name == "shift" && tc.time < 12000 {
				dark = 11
			}
			if got := srv.SkyDarkenInDimension(dim); got != dark {
				t.Errorf("%s time %d: darkening = %d, want %d", dim, tc.time, got, dark)
			}
			if burnt != burn {
				t.Errorf("%s time %d: burning = %v, want %v", dim, tc.time, burnt, burn)
			}
			c.ChatCommand("kill @e[type=zombie]")
			c.Settle()
		}
	}
}
