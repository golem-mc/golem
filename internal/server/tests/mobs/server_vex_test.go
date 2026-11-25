package mobs

import (
	"testing"

	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/internal/server"
	"github.com/golem-mc/golem/internal/server/rules"
	"github.com/golem-mc/golem/internal/server/servertest"
)

const vexKind = "minecraft:vex"

// vexWorld joins a player at the origin on Hard difficulty at night.
func vexWorld(t *testing.T) (*server.Server, *servertest.Client) {
	t.Helper()
	srv, connect := servertest.Start(t, servertest.HostileTestConfig())
	srv.SetDifficulty(rules.DifficultyHard)
	srv.SetDayTime(18000)
	c := connect().Join("Target")
	servertest.HoldTicks(t, srv)
	return srv, c
}

// TestAVexFliesThroughWallsAndChargesThePlayer: a vex flies through a stone wall at the player, the
// charging flag shows on the wire and the sword hurts.
// vanilla: Vex.VexChargeAttackGoal, Vex.VexMoveControl
func TestAVexFliesThroughWallsAndChargesThePlayer(t *testing.T) {
	t.Parallel()
	srv, c := vexWorld(t)
	stone := servertest.BlockState(t, "minecraft:stone")
	id := srv.SpawnMob(vexKind, 10.5, 65, 0.5)
	var charged, hurt, walled bool
	crossed := false
	pumpTicks(c, srv, 400, servertest.HostileSkip, func(pid int32, data []byte) bool {
		if x, _, _, ok := srv.MobPos(id); ok && walled && x < 4 {
			crossed = true
		}
		if pid == protocol.IDPlayEntityMetadata {
			r := protocol.NewReader(data)
			if r.ReadVarInt() == id {
				if entries, ok := servertest.ReadMetadata(r); ok {
					if e := servertest.MetaOf(entries, protocol.MetadataIndexVexFlags); e != nil && e.ByteVal&1 != 0 && !charged {
						charged = true
						// The wall goes up once the charge is on.
						for x := int32(4); x <= 6; x++ {
							for y := int32(60); y <= 80; y++ {
								for z := int32(-8); z <= 8; z++ {
									srv.SetBlock(x, y, z, stone)
								}
							}
						}
						walled = true
					}
				}
			}
		}
		if pid == protocol.IDPlaySetHealth && servertest.ExpectHealthReport(c, data).Health < 20 {
			hurt = true
		}
		return hurt
	})
	if !crossed {
		t.Error("the vex never passed the wall")
	}
	if hp, _ := srv.MobHealth(id); hp != 14 {
		t.Errorf("vex health = %v after flying through the wall, want 14", hp)
	}
	if !charged {
		t.Error("no metadata set the vex's charging flag (index 16)")
	}
	if !hurt {
		t.Error("the vex never hurt the player")
	}
}

// TestAVexFindsAnEvokerThatWakesAfterIt: the evoker saves in a chunk the
// rejoin never loads, so its vexes wake first and resolve to no owner; when
// the evoker's chunk wakes later the vexes link to it again from the saved
// identity, which the fangs' team rule then reads.
// vanilla: Vex.readAdditionalSaveData, EntityReference.getEntity
func TestAVexFindsAnEvokerThatWakesAfterIt(t *testing.T) {
	t.Parallel()
	hardNight := func(s *server.Server) {
		s.SetDifficulty(rules.DifficultyHard)
		s.SetDayTime(18000)
	}
	cfg := servertest.HostileTestConfig()
	cfg.LevelName = t.TempDir()
	cfg.SimulationDistance = 4
	srv, c := servertest.Join(t, cfg, "Keeper", hardNight)
	servertest.HoldTicks(t, srv)
	evoker := srv.SpawnMob(evokerKind, 10.5, 64, 0.5)
	pumpTicks(c, srv, 240, servertest.HostileSkip, func(int32, []byte) bool {
		return countOfKind(srv, vexKind) >= 3
	})
	if countOfKind(srv, vexKind) < 3 {
		t.Fatal("the evoker summoned no vexes")
	}
	// Six chunks out, past the four the joined client holds, so the restart
	// wakes the vexes' chunks and not the evoker's; inside despawn's instant
	// 128, so the woken evoker stands while the test reads it.
	// vanilla: Mob.checkDespawn
	servertest.HoldTicks(t, srv)
	srv.MoveMob(evoker, 96.5, 0.5)
	srv.FlushMobs()
	srv.Shutdown()

	restarted := *cfg
	back, reconnect := servertest.Start(t, &restarted, hardNight)
	servertest.FreezeUntilHeld(t, back)
	reconnect().Join("Keeper")
	servertest.HoldTicks(t, back)
	servertest.UntilTicks(t, back, 40, "the vexes woke", func() bool {
		return countOfKind(back, vexKind) >= 3
	})
	var vexes []int32
	for _, m := range back.Mobs() {
		if m.Kind == vexKind {
			vexes = append(vexes, m.ID)
		}
	}
	for _, v := range vexes {
		if state, ok := back.MobVexState(v); !ok || state.OwnerID != 0 {
			t.Errorf("vex %d resolved an owner (%d) before its evoker woke", v, state.OwnerID)
		}
	}
	release := back.HoldChunk(96, 0)
	defer release()
	servertest.UntilTicks(t, back, 100, "the evoker woke", func() bool {
		return countOfKind(back, evokerKind) == 1
	})
	var evokerAgain int32
	for _, m := range back.Mobs() {
		if m.Kind == evokerKind {
			evokerAgain = m.ID
		}
	}
	if evokerAgain == 0 {
		t.Fatal("the evoker did not wake in the held chunk")
	}
	servertest.UntilTicks(t, back, 100, "the vexes to find their evoker", func() bool {
		for _, v := range vexes {
			state, ok := back.MobVexState(v)
			if !ok || state.OwnerID != evokerAgain {
				return false
			}
		}
		return true
	})
	// vanilla: Evoker.considersEntityAsAlly
	back.SetMobNoAI(evokerAgain)
	back.SetMobNoAI(vexes[0])
	x, y, z, _ := back.MobPos(vexes[0])
	back.SpawnFangs(x, y, z, evokerAgain)
	servertest.RunTicks(t, back, 30)
	if hp, _ := back.MobHealth(vexes[0]); hp != 14 {
		t.Errorf("vex health = %v after its evoker's fangs, want 14", hp)
	}
}
