package redstone

import (
	"fmt"
	"testing"

	"github.com/golem-mc/golem/internal/config"
	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/internal/server"
	"github.com/golem-mc/golem/internal/server/rules"
	"github.com/golem-mc/golem/internal/server/servertest"
	"github.com/golem-mc/golem/registry"
	"github.com/google/uuid"
)

// vanilla: Entity.applyMovementEmissionAndPlaySound, AbstractMinecart.getMovementEmission
func TestARollingMinecartActivatesASculkSensor(t *testing.T) {
	for _, tc := range []struct {
		name         string
		moving, wool bool
	}{
		{name: "stationary"}, {name: "rolling", moving: true}, {name: "rolling on wool", moving: true, wool: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			cfg := servertest.BlockTestConfig()
			cfg.ViewDistance, cfg.SimulationDistance = config.MinDistance, config.MinDistance
			srv, c := servertest.Join(t, cfg, "Actor", servertest.OpsListing(t, "Actor"))
			servertest.HoldTicks(t, srv)
			rail := servertest.MustState(t, "minecraft:rail", map[string]string{"shape": "east_west"})
			for x := range int32(16) {
				srv.SetBlock(x, 64, 0, rail)
				if tc.wool {
					srv.SetBlock(x, 63, 0, servertest.MustState(t, "minecraft:white_wool", nil))
				}
			}
			motion := ""
			if tc.moving {
				motion = " {Motion:[0.4d,0d,0d]}"
			}
			c.ChatCommand("summon minecart 1.5 64.0625 0.5" + motion)
			servertest.AwaitSystemChatSaying(t, c, "commands.summon.success")
			c.Settle()
			srv.SetBlock(5, 64, 3, servertest.MustState(t, "minecraft:sculk_sensor", nil))
			powered := func() bool { return vehicleSensorPowered(srv) }
			if tc.moving && !tc.wool {
				servertest.UntilTicks(t, srv, 40, "the rolling cart activates the sensor", powered)
			} else {
				for range 40 {
					servertest.RunTicks(t, srv, 1)
					if powered() {
						t.Fatal("a stationary cart or one on wool activated the sensor")
					}
				}
			}
		})
	}
}

// vanilla: AbstractBoat.getMovementEmission, ServerGamePacketListenerImpl.handleMoveVehicle
func TestBoatStridesActivateASculkSensor(t *testing.T) {
	for _, water := range []bool{false, true} {
		for _, driven := range []bool{false, true} {
			t.Run(fmt.Sprintf("water=%t/driven=%t", water, driven), func(t *testing.T) {
				t.Parallel()
				cfg := servertest.BlockTestConfig()
				cfg.ViewDistance, cfg.SimulationDistance = config.MinDistance, config.MinDistance
				srv, c := servertest.Join(t, cfg, "Actor", servertest.OpsListing(t, "Actor"))
				servertest.HoldTicks(t, srv)
				ground := servertest.MustState(t, "minecraft:ice", nil)
				if water {
					ground = servertest.MustState(t, "minecraft:water", nil)
				}
				for x := range int32(16) {
					for z := range int32(3) {
						srv.SetBlock(x, 63, z, ground)
					}
				}
				y := 64.0
				if water {
					y = 63.55
				}
				c.ChatCommand(fmt.Sprintf("summon oak_boat 1.5 %g 1.5 {Motion:[0.8d,0d,0d]}", y))
				servertest.AwaitSystemChatSaying(t, c, "commands.summon.success")
				if driven {
					c.ChatCommand("ride @s mount @e[type=oak_boat,limit=1]")
					servertest.AwaitSystemChatSaying(t, c, "commands.ride.mount.success")
				}
				c.Settle()
				event := rules.GameEventStep
				affected := ground
				if water {
					event, affected = rules.GameEventSwim, 0
				}
				checkContext := vehicleStrideContext(t, srv, event, affected)

				srv.SetBlock(5, 64, 3, servertest.MustState(t, "minecraft:sculk_sensor", nil))
				if driven {
					for i := 1; i <= 4; i++ {
						c.Send(protocol.IDPlayMoveVehicleIn, func(b *protocol.Buffer) {
							b.PutF64(1.5 + float64(i)*0.5)
							b.PutF64(y)
							b.PutF64(1.5)
							b.PutF32(90)
							b.PutF32(0)
							b.PutBool(!water)
						})
						c.Settle()
					}
				}
				servertest.UntilTicks(t, srv, 40, "the moving boat activates the sensor", func() bool {
					return vehicleSensorPowered(srv)
				})
				checkContext()
			})
		}
	}
}

func vehicleSensorPowered(srv *server.Server) bool {
	power, _ := registry.StateInt(srv.BlockAt(5, 64, 3), registry.PropPower)
	return power > 0
}

func vehicleStrideContext(t *testing.T, srv *server.Server, event rules.GameEvent, affected uint16) func() {
	t.Helper()
	contexts := make(chan rules.GameEventContext, 1)
	srv.ObserveGameEventContexts(func(got string, _, _, _ float64, ctx rules.GameEventContext) {
		if got == event.Name() {
			select {
			case contexts <- ctx:
			default:
			}
		}
	})
	return func() {
		t.Helper()
		select {
		case ctx := <-contexts:
			if ctx.Mob == uuid.Nil || ctx.Player != uuid.Nil {
				t.Errorf("vehicle event source = %+v, want vehicle UUID without a player", ctx)
			}
			if ctx.Affected != affected {
				t.Errorf("affected block state = %d, want %d", ctx.Affected, affected)
			}
		default:
			t.Errorf("no %s vehicle event", event.Name())
		}
	}
}
