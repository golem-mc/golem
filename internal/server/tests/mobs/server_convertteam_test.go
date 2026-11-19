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

// TestALightningStruckPigKeepsItsTeam strikes a pig on team red: the
// zombified piglin takes its place on the team.
// vanilla: Pig.thunderHit, ConversionType.convertCommon
func TestALightningStruckPigKeepsItsTeam(t *testing.T) {
	t.Parallel()
	srv, _ := servertest.Join(t, servertest.SurvivalTestConfig(), "Struck")
	srv.StopTick()
	srv.SetDifficulty(2)
	pos := srv.PlayerPosition("Struck")
	pig := srv.SpawnMob("minecraft:pig", pos.X+1, pos.Y, pos.Z)
	old, _ := srv.MobUUID(pig)
	srv.PutOnTeam("red", old)

	srv.StrikeLightning(pos.X+1, pos.Y, pos.Z)
	srv.TickBolts(1)

	ids := srv.MobIDsOfKind("minecraft:zombified_piglin")
	if len(ids) != 1 {
		t.Fatalf("zombified piglins after the bolt = %v, want one", ids)
	}
	want, _ := srv.MobUUID(ids[0])
	if got := redMembers(t, srv); got != want {
		t.Errorf("red's member after the bolt is %q, want the zombified piglin's %s (the pig was %s)", got, want, old)
	}
}

// TestAConvertedMobKeepsDropChancesOnlyOfItsEquipment drowns a zombie wearing
// boots: the drowned keeps the boots' chance, not the empty slots' ones.
// vanilla: ConversionType.convertCommon
func TestAConvertedMobKeepsDropChancesOnlyOfItsEquipment(t *testing.T) {
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
	srv.RunLine(`summon minecraft:zombie 3.5 64 0.5 {DrownedConversionTime:1,equipment:{feet:{id:"minecraft:iron_boots",count:1}},drop_chances:{body:0.5f,saddle:3f,mainhand:2f,feet:2f}}`)
	servertest.WaitFor(t, srv, 20, func() bool { return len(srv.MobIDsOfKind("minecraft:drowned")) == 1 })
	out := srv.RunLine("data get entity @e[type=drowned,limit=1] drop_chances")
	if len(out) != 1 || len(out[0].With) != 2 || out[0].With[1].Plain() != "{feet: 2.0f}" {
		t.Errorf("drowned drop_chances = %+v, want {feet: 2.0f}", out)
	}
}
