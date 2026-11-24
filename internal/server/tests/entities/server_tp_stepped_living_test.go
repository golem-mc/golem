package entities

import (
	"fmt"
	"slices"
	"strings"
	"testing"

	"github.com/golem-mc/golem/internal/config"
	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/internal/server/servertest"
)

// An armor stand or mannequin, both LivingEntity, reaches the client on a
// stepped path whose duration is the ticks since its previous movement tick,
// idle ones included, so an idle entity's next teleport takes one interval.
// vanilla: SteppedInterpolationTracker, ServerEntity.sendChanges
func TestTeleportCommandSteppedPathLastsOneTrackingInterval(t *testing.T) {
	t.Parallel()
	for _, kind := range []struct {
		name     string
		interval int32
	}{{"armor_stand", 3}, {"mannequin", 2}} {
		for _, frozen := range []bool{false, true} {
			t.Run(fmt.Sprintf("%s/frozen=%v", kind.name, frozen), func(t *testing.T) {
				t.Parallel()
				cfg := servertest.HostileTestConfig()
				cfg.ViewDistance, cfg.SimulationDistance = config.MinDistance, config.MinDistance
				srv, c := servertest.Join(t, cfg, "Watcher")
				servertest.HoldTicks(t, srv)
				srv.RunLine(fmt.Sprintf(`summon %s 0.5 100 0.5 {Tags:[s],NoGravity:1b}`, kind.name))
				servertest.RunTicks(t, srv, 1)
				c.Settle()
				if frozen {
					srv.RunLine(`tick freeze`)
				}
				var sync servertest.EntityPositionSync
				var id int32
				for i, y := range []float64{103, 112} {
					if i > 0 {
						// A multiple of both intervals, so the teleport lands
						// between two scheduled gates.
						servertest.RunTicks(t, srv, 6)
					}
					srv.RunLine(fmt.Sprintf(`tp @e[tag=s,limit=1] 0.5 %v 0.5`, y))
					seen := c.ReadTicks(srv, 8, func(pid int32, data []byte) bool {
						if pid != protocol.IDPlayTeleportEntity {
							return false
						}
						sync = servertest.ParseEntityPositionSync(data)
						return sync.Y == y && (i == 0 || sync.ID == id)
					})
					if i == 0 {
						id = sync.ID
					}
					if !seen {
						t.Fatalf("teleport %d: no Teleport Entity at y %v", i, y)
					}
				}
				if !sync.Stepped || sync.StepTicks != kind.interval {
					t.Errorf("second sync = %+v, want stepped over %d ticks", sync, kind.interval)
				}
			})
		}
	}
}

// A change to an armor stand's or mannequin's synched data, or a /data load,
// opens one movement tick off the period and is consumed by whichever tick
// opens: the teleport that follows waits for the next scheduled tick, and its
// path lasts the ticks since the pass the change opened. A change that leaves
// the synched value as it was opens nothing, and a changed attribute alone
// waits for the next movement tick.
// vanilla: ServerEntity.sendChanges, Entity.needsSync, SteppedInterpolationTracker,
// SynchedEntityData.set
func TestDirtyDataOpensOneMovementTick(t *testing.T) {
	t.Parallel()
	const (
		stand     = 1
		mannequin = 2
	)
	cases := []struct {
		name        string
		kinds       int
		setup, line string
		opens       bool // the change is a synched one
		attributes  bool // the change dirties an attribute
	}{
		{"glow", stand | mannequin, ``, `effect give @e[tag=s,limit=1] glowing infinite 0 true`, true, false},
		{"load", stand | mannequin, ``, `data merge entity @e[tag=s,limit=1] {Invulnerable:1b}`, true, false},
		{"health", mannequin, ``, `damage @e[tag=s,limit=1] 1 generic`, true, false},
		{"zero_damage", mannequin, ``, `damage @e[tag=s,limit=1] 0 generic`, false, false},
		{"visible_refresh", mannequin, `effect give @e[tag=s,limit=1] speed 100 0 false`, `effect give @e[tag=s,limit=1] speed 200 0 false`, true, false},
		{"hidden_refresh", mannequin, `effect give @e[tag=s,limit=1] speed 100 0 true`, `effect give @e[tag=s,limit=1] speed 200 0 true`, false, false},
		// The first hidden effect turns the ambient flag on.
		{"hidden_first", mannequin, ``, `effect give @e[tag=s,limit=1] speed 100 0 true`, true, false},
		// A refresh whose particle option vanilla reuses changes nothing.
		{"omen_refresh", mannequin, `effect give @e[tag=s,limit=1] trial_omen 100 0 false`, `effect give @e[tag=s,limit=1] trial_omen 200 0 false`, false, false},
		// The modifier changes, the synched fields do not.
		{"hidden_amplifier", mannequin, `effect give @e[tag=s,limit=1] speed 100 0 true`, `effect give @e[tag=s,limit=1] speed 100 1 true`, false, true},
		// The health is healed back before tracking, but stays dirty.
		{"damage_healed", mannequin, ``, "damage @e[tag=s,limit=1] 1 generic\neffect give @e[tag=s,limit=1] instant_health 1 0 true", true, false},
	}
	for _, kind := range []struct {
		name     string
		bit      int
		interval int
	}{{"armor_stand", stand, 3}, {"mannequin", mannequin, 2}} {
		t.Run(kind.name, func(t *testing.T) {
			t.Parallel()
			cfg := servertest.HostileTestConfig()
			cfg.ViewDistance, cfg.SimulationDistance = config.MinDistance, config.MinDistance
			srv, c := servertest.Join(t, cfg, "Watcher")
			servertest.HoldTicks(t, srv)
			for _, tc := range cases {
				if tc.kinds&kind.bit == 0 {
					continue
				}
				for _, scheduled := range []bool{true, false} {
					{
						t.Run(fmt.Sprintf("%s/scheduled=%v", tc.name, scheduled), func(t *testing.T) {
							srv.RunLine(`kill @e[tag=s]`)
							srv.RunLine(fmt.Sprintf(`summon %s 0.5 100 0.5 {Tags:[s],NoGravity:1b,Health:20f}`, kind.name))
							if tc.setup != "" {
								srv.RunLine(tc.setup)
							}
							servertest.RunTicks(t, srv, 1)
							c.Settle()
							// Calibrate: a lone teleport arrives on a scheduled
							// tick, and the entity ID comes with it.
							srv.RunLine(`tp @e[tag=s,limit=1] 0.5 101 0.5`)
							var id int32
							if !c.ReadTicks(srv, kind.interval, func(pid int32, data []byte) bool {
								if pid != protocol.IDPlayTeleportEntity {
									return false
								}
								sync := servertest.ParseEntityPositionSync(data)
								id = sync.ID
								return sync.Y == 101
							}) {
								t.Fatal("no calibration teleport")
							}
							c.Settle()
							// The next scheduled tick is interval ticks on.
							idle := kind.interval - 1
							if !scheduled {
								idle = 0
							}
							servertest.RunTicks(t, srv, idle)
							for line := range strings.SplitSeq(tc.line, "\n") {
								srv.RunLine(line)
							}
							attributesOf := func(pid int32, data []byte) bool {
								return pid == protocol.IDPlayUpdateAttributes && protocol.NewReader(data).ReadVarInt() == id
							}
							// The tick the change opens or shares.
							attributesNow := c.ReadTicks(srv, 1, attributesOf)
							c.Settle()
							if tc.attributes && attributesNow != scheduled {
								t.Errorf("attributes on the change's tick: %v, want %v", attributesNow, scheduled)
							}
							attributesLater := false
							srv.RunLine(`tp @e[tag=s,limit=1] 0.5 105 0.5`)
							// The dirty pass is consumed, so the teleport waits for
							// the next scheduled tick, the ticks since the pass.
							var sync servertest.EntityPositionSync
							if !c.ReadTicks(srv, kind.interval, func(pid int32, data []byte) bool {
								attributesLater = attributesLater || attributesOf(pid, data)
								if pid != protocol.IDPlayTeleportEntity {
									return false
								}
								sync = servertest.ParseEntityPositionSync(data)
								return sync.ID == id && sync.Y == 105
							}) {
								t.Fatal("no Teleport Entity")
							}
							// The attributes follow the movement within its tick.
							attributesLater = c.ReadTicks(srv, 0, attributesOf) || attributesLater
							if tc.attributes && attributesLater == scheduled {
								t.Errorf("attributes with the next scheduled tick: %v, want %v", attributesLater, !scheduled)
							}
							want := int32(kind.interval)
							if tc.opens && !scheduled {
								want = int32(kind.interval - 1)
							}
							if !sync.Stepped || sync.StepTicks != want {
								t.Errorf("sync = %+v, want stepped over %d ticks", sync, want)
							}
						})
					}
				}
			}
		})
	}
}

// A synched change and a teleport that share a pass reach a watcher as the
// movement first and the metadata after it, running or frozen.
// vanilla: ServerEntity.sendChanges
func TestDirtyDataFollowsMovement(t *testing.T) {
	t.Parallel()
	for _, kind := range []struct {
		name     string
		interval int
		lines    []string
	}{
		{"armor_stand", 3, []string{`effect give @e[tag=s,limit=1] glowing infinite 0 true`}},
		{"mannequin", 2, []string{`effect give @e[tag=s,limit=1] glowing infinite 0 true`, `damage @e[tag=s,limit=1] 1 generic`, `effect give @e[tag=s,limit=1] speed 100 0 false`}},
	} {
		t.Run(kind.name, func(t *testing.T) {
			t.Parallel()
			cfg := servertest.HostileTestConfig()
			cfg.ViewDistance, cfg.SimulationDistance = config.MinDistance, config.MinDistance
			srv, c := servertest.Join(t, cfg, "Watcher")
			servertest.HoldTicks(t, srv)
			for _, line := range kind.lines {
				for _, frozen := range []bool{false, true} {
					t.Run(fmt.Sprintf("%s/frozen=%v", line, frozen), func(t *testing.T) {
						srv.RunLine(`kill @e[tag=s]`)
						srv.RunLine(fmt.Sprintf(`summon %s 0.5 100 0.5 {Tags:[s],NoGravity:1b,Health:20f}`, kind.name))
						servertest.RunTicks(t, srv, 1)
						c.Settle()
						if frozen {
							srv.RunLine(`tick freeze`)
							defer srv.RunLine(`tick unfreeze`)
						}
						srv.RunLine(`tp @e[tag=s,limit=1] 0.5 101 0.5`)
						var id int32
						if !c.ReadTicks(srv, kind.interval, func(pid int32, data []byte) bool {
							if pid != protocol.IDPlayTeleportEntity {
								return false
							}
							sync := servertest.ParseEntityPositionSync(data)
							id = sync.ID
							return sync.Y == 101
						}) {
							t.Fatal("no calibration teleport")
						}
						c.Settle()
						// The tick after a scheduled one is off the period.
						srv.RunLine(line)
						srv.RunLine(`tp @e[tag=s,limit=1] 0.5 105 0.5`)
						metaFirst := false
						if !c.ReadTicks(srv, kind.interval, func(pid int32, data []byte) bool {
							switch pid {
							case protocol.IDPlayEntityMetadata:
								metaFirst = metaFirst || servertest.MetadataEntityID(data) == id
							case protocol.IDPlayTeleportEntity:
								sync := servertest.ParseEntityPositionSync(data)
								return sync.ID == id && sync.Y == 105
							}
							return false
						}) {
							t.Fatal("no Teleport Entity")
						}
						if metaFirst {
							t.Error("metadata arrived before the movement")
						}
						// The movement is followed by the data, then the attributes.
						var after []string
						c.ReadTicks(srv, 0, func(pid int32, data []byte) bool {
							if pid == protocol.IDPlayEntityMetadata && servertest.MetadataEntityID(data) == id {
								after = append(after, "meta")
							}
							if pid == protocol.IDPlayUpdateAttributes && protocol.NewReader(data).ReadVarInt() == id {
								after = append(after, "attributes")
							}
							return false
						})
						want := []string{"meta"}
						if strings.Contains(line, "speed") {
							want = append(want, "attributes")
						}
						if !slices.Equal(after, want) {
							t.Errorf("after the movement: %v, want %v", after, want)
						}
					})
				}
			}
		})
	}
}
