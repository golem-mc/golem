package commands

import (
	"slices"
	"strconv"
	"testing"

	"github.com/google/uuid"

	"github.com/golem-mc/golem/internal/config"
	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/internal/server/entity"
	"github.com/golem-mc/golem/internal/server/servertest"
)

// angleByte is the protocol angle of degrees.
func angleByte(degrees float64) uint8 {
	var b protocol.Buffer
	b.PutAngle(float32(degrees))
	return b.Bytes()[0]
}

// A chicken carrying a pig carrying a cow, each facing its own way, so a rider
// that took its mount's angles or its mount's clamped pitch reads wrong.
// vanilla: Entity.calculatePassengerTransition, TeleportCommand.performTeleport
func TestTeleportTurnsMobPassengerWithMount(t *testing.T) {
	t.Parallel()
	const summon = `summon chicken 3.5 64 3.5 {NoAI:1b,NoGravity:1b,Rotation:[170f,-80f],Passengers:[` +
		`{id:"minecraft:pig",NoAI:1b,NoGravity:1b,Rotation:[0f,0f],Passengers:[` +
		`{id:"minecraft:cow",NoAI:1b,NoGravity:1b,Rotation:[-60f,-40f]}]}]}`
	const relative = "execute rotated 0 0 run tp @e[type=chicken,limit=1] 6.5 64 6.5 ~-170 ~120"
	type rot struct{ yaw, pitch float64 }
	cases := []struct {
		name          string
		tps           []string
		chicken       string
		pig, cow      rot
		frozen, level bool
	}{
		// The pig's pitch clamps to 90 but the cow's turn adds to the
		// unclamped 110.
		{name: "absolute turn clamps each rider", tps: []string{"tp @e[type=chicken] 6.5 64 6.5 180 30"},
			chicken: "[-180.0f, 30.0f]", pig: rot{-350, 90}, cow: rot{-410, 70}},
		{name: "relative turn wraps its offset", tps: []string{relative},
			chicken: "[190.0f, -90.0f]", pig: rot{20, -90}, cow: rot{-40, -90}},
		{name: "relative turn while frozen", tps: []string{relative}, frozen: true,
			chicken: "[190.0f, -90.0f]", pig: rot{20, -90}, cow: rot{-40, -90}},
		// The second teleport turns the riders from the mount's stored,
		// clamped rotation.
		{name: "absolute after relative", tps: []string{relative, "tp @e[type=chicken,limit=1] 6.5 64 6.5 0 -20"},
			chicken: "[0.0f, -20.0f]", pig: rot{-170, -20}, cow: rot{-230, -20}},
		{name: "facing turns only the mount", tps: []string{"tp @e[type=chicken] 6.5 64 6.5 facing 100 64 6.5"}, level: true,
			chicken: "[-90.0f, 0.0f]",
			pig:     rot{0, 0}, cow: rot{-60, -40}},
	}
	interval := max(entity.MobUpdateInterval(entity.TypePig), entity.MobUpdateInterval(entity.TypeCow))
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			cfg := servertest.BlockTestConfig()
			cfg.ViewDistance = config.MinDistance
			cfg.SimulationDistance = config.MinDistance
			srv, c := servertest.Join(t, cfg, "Summoner", servertest.OpsListing(t, "Summoner"))
			servertest.HoldTicks(t, srv)
			c.Settle()
			c.ChatCommand(summon)
			ids := map[int32]int32{}
			uuids := map[int32]uuid.UUID{}
			seats := map[int32][]int32{}
			collect := func(id int32, data []byte) {
				switch id {
				case protocol.IDPlaySpawnEntity:
					r := protocol.NewReader(data)
					eid, u := r.ReadVarInt(), r.ReadUUID()
					kind := r.ReadVarInt()
					ids[kind], uuids[kind] = eid, u
				case protocol.IDPlaySetPassengers:
					vehicle, riders := servertest.ReadPassengers(c, data)
					seats[vehicle] = riders
				}
			}
			for range 3 {
				c.SettleAndCollect(collect)
				if len(seats) == 2 {
					break
				}
			}
			chicken, pig, cow := ids[protocol.EntityTypeChicken], ids[protocol.EntityTypePig], ids[protocol.EntityTypeCow]
			if !slices.Contains(seats[chicken], pig) || !slices.Contains(seats[pig], cow) {
				t.Fatalf("the riders were not seated: %v", seats)
			}
			if tc.frozen {
				srv.RunLine("tick freeze")
			}
			want := map[int32]rot{pig: tc.pig, cow: tc.cow}
			turned := map[int32]bool{}
			for i, tp := range tc.tps {
				last := i == len(tc.tps)-1
				c.ChatCommand(tp)
				// ReadTicks passes on what the command sent before its ticks.
				c.ReadTicks(srv, interval, func(id int32, data []byte) bool {
					switch id {
					case protocol.IDPlayEntityRotation:
						r := protocol.NewReader(data)
						eid := r.ReadVarInt()
						w, ok := want[eid]
						if !ok || !last {
							break
						}
						r.ReadBool() // on ground
						yaw, pitch := r.ReadU8(), r.ReadU8()
						// A level case sends nothing: any turn of a rider is wrong.
						if tc.level || yaw == angleByte(w.yaw) && pitch == angleByte(w.pitch) {
							turned[eid] = true
						}
					case protocol.IDPlaySetPassengers:
						vehicle, riders := servertest.ReadPassengers(c, data)
						if vehicle == chicken && !slices.Contains(riders, pig) || vehicle == pig && !slices.Contains(riders, cow) {
							t.Errorf("%q unseated a rider: vehicle %d now carries %v", tp, vehicle, riders)
						}
					}
					return false
				})
			}
			for eid, w := range want {
				switch {
				case tc.level && turned[eid]:
					t.Errorf("a facing clause turned rider %d", eid)
				case !tc.level && !turned[eid]:
					t.Errorf("rider %d's turn to %v, %v was not sent while seated", eid, w.yaw, w.pitch)
				}
			}
			query := func(name string, kind int32, w string) {
				c.ChatCommand("data get entity @e[type=" + name + ",limit=1] Rotation")
				c.ExpectCommandFeedbackText("commands.data.entity.query",
					servertest.EntityDisplayComponent("minecraft:"+name, "entity.minecraft."+name, uuids[kind]), servertest.NBTArg(w))
			}
			query("chicken", protocol.EntityTypeChicken, tc.chicken)
			query("pig", protocol.EntityTypePig, nbtRotation(tc.pig.yaw, tc.pig.pitch))
			query("cow", protocol.EntityTypeCow, nbtRotation(tc.cow.yaw, tc.cow.pitch))
		})
	}
}

// nbtRotation is how /data prints a Rotation of whole degrees.
func nbtRotation(yaw, pitch float64) string {
	return "[" + strconv.FormatFloat(yaw, 'f', 1, 64) + "f, " + strconv.FormatFloat(pitch, 'f', 1, 64) + "f]"
}
