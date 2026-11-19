package mobs

import (
	"testing"

	"github.com/golem-mc/golem/internal/config"
	"github.com/golem-mc/golem/internal/server"
	"github.com/golem-mc/golem/internal/server/servertest"
	"github.com/golem-mc/golem/registry"
)

// redMembers is what /team list red names: its one member's UUID, "" when
// it has none, and the whole answer otherwise.
func redMembers(t *testing.T, srv *server.Server) string {
	t.Helper()
	out := srv.RunLine("team list red")
	if len(out) == 1 && out[0].Translate == "commands.team.list.members.empty" {
		return ""
	}
	if len(out) != 1 || out[0].Translate != "commands.team.list.members.success" ||
		len(out[0].With) != 3 || out[0].With[1].Number == nil || *out[0].With[1].Number != 1 {
		t.Fatalf("team list red = %+v, want one member", out)
	}
	return out[0].With[2].Text
}

// TestADrownedZombieKeepsItsTeam drowns a zombie on team red: the drowned
// takes its place on the team.
// vanilla: ConversionType.convertCommon
func TestADrownedZombieKeepsItsTeam(t *testing.T) {
	t.Parallel()
	cfg := servertest.HostileTestConfig()
	cfg.ViewDistance, cfg.SimulationDistance = config.MinDistance, config.MinDistance
	srv, _ := servertest.Join(t, cfg, "Diver")
	srv.SetSpawning(false)
	srv.SetDayTime(18000)
	water, _ := registry.FindState("minecraft:water", nil)
	stone, _ := registry.FindState("minecraft:stone", nil)
	for x := int32(2); x <= 6; x++ {
		for z := int32(-2); z <= 2; z++ {
			for y := int32(64); y <= 67; y++ {
				if x == 2 || x == 6 || z == -2 || z == 2 {
					srv.SetBlock(x, y, z, stone)
				} else {
					srv.SetBlock(x, y, z, water)
				}
			}
		}
	}
	srv.RunLine("team add red")
	const old = "00000001-0000-0002-0000-000300000004"
	srv.RunLine(`summon minecraft:zombie 3.5 64 0.5 {UUID:[I;1,2,3,4],DrownedConversionTime:1,Team:"red"}`)
	if got := redMembers(t, srv); got != old {
		t.Fatalf("red's member before the drowning is %q, want the zombie's %s", got, old)
	}
	var drowned int32
	servertest.WaitFor(t, srv, 20, func() bool {
		drowned = 0
		if ids := srv.MobIDsOfKind("minecraft:drowned"); len(ids) == 1 {
			drowned = ids[0]
		}
		return drowned != 0
	})
	want, _ := srv.MobUUID(drowned)
	if got := redMembers(t, srv); got != want {
		t.Errorf("red's member after the drowning is %q, want the drowned's %s", got, want)
	}
}
