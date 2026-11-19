package mobs

import (
	"testing"

	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/internal/server"
	"github.com/golem-mc/golem/internal/server/rules"
	"github.com/golem-mc/golem/internal/server/servertest"
	"github.com/golem-mc/golem/internal/server/world"
	"github.com/golem-mc/golem/nbt"
	"github.com/golem-mc/golem/registry"
	"github.com/google/uuid"
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

// TestAVexWithoutLifeStarves: a vex whose limited life ran out takes a point
// of starve damage a second; one with no limit does not.
// vanilla: Vex.tick
func TestAVexWithoutLifeStarves(t *testing.T) {
	t.Parallel()
	srv, c := vexWorld(t)
	c.MoveToGround(60.5, 64, 60.5, true)
	limited := srv.SpawnMob(vexKind, 10.5, 70, 0.5)
	free := srv.SpawnMob(vexKind, 10.5, 72, 4.5)
	srv.SetMobVexLife(limited, 5)
	srv.TickMobs(5 + 20)
	if hp, _ := srv.MobHealth(limited); hp != 12 {
		t.Errorf("limited vex health = %v after its life and a beat, want 12", hp)
	}
	if hp, _ := srv.MobHealth(free); hp != 14 {
		t.Errorf("unlimited vex health = %v, want 14", hp)
	}
	srv.TickMobs(20 * 20)
	if _, alive := srv.MobHealth(limited); alive {
		t.Error("the limited vex outlived its starvation")
	}
}

// TestAVexKeepsItsLifeAndBoundBlockAcrossARestart: the limited life and the
// bound block ride the saved compound, and the restarted vex keeps counting
// from them.
// vanilla: Vex.addAdditionalSaveData
func TestAVexKeepsItsLifeAndBoundBlockAcrossARestart(t *testing.T) {
	t.Parallel()
	cfg := servertest.SurvivalConfig()
	cfg.LevelName = t.TempDir()
	srv, _ := servertest.Join(t, cfg, "Keeper")
	// Held, so no tick counts the life down before the save.
	servertest.HoldTicks(t, srv)
	id := srv.SpawnMob(vexKind, 8.5, 70, 8.5)
	srv.SetMobVexLife(id, 400)
	srv.SetMobVexBound(id, 5, 71, -3)
	srv.FlushMobs()
	srv.Shutdown()

	servertest.EntityFileIs(t, cfg.LevelName, world.ChunkPos{X: 0, Z: 0}, vexKind, func(compound nbt.Compound) {
		if ticks, ok := nbt.Get[nbt.Int](compound, "life_ticks"); !ok || ticks != 400 {
			t.Errorf("life_ticks = %v, %v; want 400", ticks, ok)
		}
		if pos, ok := nbt.Get[nbt.IntArray](compound, "bound_pos"); !ok || len(pos) != 3 || pos[0] != 5 || pos[1] != 71 || pos[2] != -3 {
			t.Errorf("bound_pos = %v, %v; want [5 71 -3]", pos, ok)
		}
	})

	restarted := *cfg
	back, reconnect := servertest.Start(t, &restarted)
	servertest.FreezeUntilHeld(t, back)
	reconnect().Join("Keeper")
	servertest.HoldTicks(t, back)
	servertest.UntilTicks(t, back, 40, "the vex woke", func() bool { return back.MobCount() > 0 })
	for _, m := range back.Mobs() {
		if m.Kind != vexKind {
			continue
		}
		v, ok := back.MobVexState(m.ID)
		if !ok || !v.Limited || v.LifeTicks > 400 || v.LifeTicks < 300 {
			t.Errorf("restarted vex = %+v, %v; want a limited life near 400", v, ok)
		}
		if !v.BoundSet || v.BoundX != 5 || v.BoundY != 71 || v.BoundZ != -3 {
			t.Errorf("restarted vex bound = %+v; want (5, 71, -3)", v)
		}
		return
	}
	t.Error("the vex did not come back after the restart")
}

// TestAVexKeepsItsEvokerAcrossARestart: the summon's owner rides the saved
// compound as the evoker's identity, and the restarted vex is owned again:
// it takes its evoker's target, which the fangs' team rule then spares.
// vanilla: Vex.addAdditionalSaveData, Vex.readAdditionalSaveData
func TestAVexKeepsItsEvokerAcrossARestart(t *testing.T) {
	t.Parallel()
	hardNight := func(s *server.Server) {
		s.SetDifficulty(rules.DifficultyHard)
		s.SetDayTime(18000)
	}
	cfg := servertest.HostileTestConfig()
	cfg.LevelName = t.TempDir()
	srv, c := servertest.Join(t, cfg, "Keeper", hardNight)
	servertest.HoldTicks(t, srv)
	evoker := srv.SpawnMob(evokerKind, 10.5, 64, 0.5)
	pumpTicks(c, srv, 240, servertest.HostileSkip, func(int32, []byte) bool {
		return countOfKind(srv, vexKind) >= 3
	})
	if countOfKind(srv, vexKind) < 3 {
		t.Fatal("the evoker summoned no vexes")
	}
	identity, ok := srv.MobUUID(evoker)
	if !ok {
		t.Fatal("the evoker walked under no UUID")
	}
	evokerUUID, err := uuid.Parse(identity)
	if err != nil {
		t.Fatalf("the evoker's UUID %q does not parse", identity)
	}
	servertest.HoldTicks(t, srv)
	srv.FlushMobs()
	srv.Shutdown()

	// Each vex's compound sits in the chunk the vex itself saved in.
	for _, m := range srv.Mobs() {
		if m.Kind != vexKind {
			continue
		}
		x, _, z, ok := srv.MobPos(m.ID)
		if !ok {
			continue
		}
		pos := world.ChunkPos{X: world.ChunkOf(x), Z: world.ChunkOf(z)}
		servertest.EntityFileIs(t, cfg.LevelName, pos, vexKind, func(compound nbt.Compound) {
			owner, _ := nbt.Get[nbt.IntArray](compound, "owner")
			if words, hasOwner := nbt.UUIDFromIntArray(owner); !hasOwner || words != evokerUUID {
				t.Errorf("saved vex owner = %v; want the evoker's UUID %s", owner, evokerUUID)
			}
		})
	}

	restarted := *cfg
	back, reconnect := servertest.Start(t, &restarted, hardNight)
	servertest.FreezeUntilHeld(t, back)
	player := reconnect().Join("Keeper")
	servertest.HoldTicks(t, back)
	servertest.UntilTicks(t, back, 40, "the vexes woke", func() bool { return back.MobCount() >= 4 })
	var evokerAgain int32
	var vexes []int32
	for _, m := range back.Mobs() {
		switch m.Kind {
		case evokerKind:
			evokerAgain = m.ID
		case vexKind:
			vexes = append(vexes, m.ID)
		}
	}
	if evokerAgain == 0 || len(vexes) == 0 {
		t.Fatalf("the evoker (%d) or its vexes (%v) did not wake", evokerAgain, vexes)
	}
	for _, v := range vexes {
		state, ok := back.MobVexState(v)
		if !ok || state.OwnerID != evokerAgain {
			t.Errorf("restarted vex %d = %+v, %v; want the evoker %d back as owner", v, state, ok, evokerAgain)
		}
	}
	servertest.UntilTicks(t, back, 200, "the evoker chose its target", func() bool {
		return back.MobTargetID(evokerAgain) == player.PlayerEntityID
	})
	servertest.UntilTicks(t, back, 200, "the vex to copy the evoker's target", func() bool {
		return back.MobTargetID(vexes[0]) == player.PlayerEntityID
	})
	// The evoker's own fangs spare its restarted vex, as its owner link names
	// it an ally. vanilla: Evoker.considersEntityAsAlly
	back.SetMobNoAI(vexes[0])
	x, y, z, _ := back.MobPos(vexes[0])
	back.SpawnFangs(x, y, z, evokerAgain)
	servertest.RunTicks(t, back, 30)
	if hp, _ := back.MobHealth(vexes[0]); hp != 14 {
		t.Errorf("restarted vex health = %v after its evoker's fangs, want 14", hp)
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

// TestAVexHasItsVanillaBody: 14 health and an iron sword.
// vanilla: Vex.createAttributes, Vex.populateDefaultEquipmentSlots
func TestAVexHasItsVanillaBody(t *testing.T) {
	t.Parallel()
	srv, _ := vexWorld(t)
	vex := srv.SpawnMob(vexKind, 44.5, 64, 40.5)
	if hp, _ := srv.MobHealth(vex); hp != 14 {
		t.Errorf("vex health = %v, want 14", hp)
	}
	iron, _ := registry.ItemID("minecraft:iron_sword")
	if got := srv.MobHand(vex); got.ItemID != iron {
		t.Errorf("vex main hand = item %d, want the iron sword %d", got.ItemID, iron)
	}
}
