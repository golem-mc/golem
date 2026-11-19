package combat

import (
	"bytes"
	"slices"
	"testing"
	"time"

	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/internal/server/player"
	"github.com/golem-mc/golem/internal/server/servertest"
	"github.com/golem-mc/golem/internal/server/world"
	"github.com/golem-mc/golem/nbt"
	"github.com/golem-mc/golem/registry"
)

// The invulnerability tests: a body spawned with Invulnerable:1b turns aside
// every ordinary harm — a survival player's swing, an arrow and a blast — with
// no damage event or flinch; a blast still shoves it, as vanilla's explosion
// pushes whatever stands in range, while a creative player's own blow and
// /kill still reach it, and the flag rides a save and a load.

// packetEntityID reads the entity ID a hurt telling opens with; false where
// the payload is not one.
func packetEntityID(data []byte) (int32, bool) {
	id, _, err := protocol.ReadVarInt(bytes.NewReader(data))
	return id, err == nil
}

// cowShoved reports whether a Set Entity Velocity lifts its body, which a
// blow's knockback does and a cow settling to the floor does not.
func cowShoved(data []byte) bool {
	r := protocol.NewReader(data)
	r.ReadVarInt()
	_, vy, _ := r.ReadLpVec3()
	return vy > 0
}

// TestAnInvulnerableMobIgnoresASurvivalSwing summons an invulnerable cow and
// swings a survival player's blade at it: nothing of a landed blow arrives —
// no damage event, no flinch, no shove — and the cow is left at its full
// health.
func TestAnInvulnerableMobIgnoresASurvivalSwing(t *testing.T) {
	t.Parallel()
	srv, c, _ := servertest.JoinAs(t, servertest.CombatTestConfig(), "Bruiser", servertest.OpsListing(t, "Bruiser"))
	c.MoveToGround(2.5, 64, 5.5, true)
	c.Settle()

	c.ChatCommand(`summon minecraft:cow 5.5 64 5.5 {Invulnerable:1b}`)
	mob := c.ExpectMobSpawn(protocol.EntityTypeCow)

	srv.GiveSlot("Bruiser", player.HotbarSlot, protocol.ItemStack{
		ItemID: servertest.MustItemID(t, "minecraft:diamond_sword"), Count: 1,
	})
	servertest.AwaitFullSwing(t, srv, "Bruiser")
	servertest.HoldTicks(t, srv)
	c.Attack(mob.EntityID)
	c.ExpectNoneWithinTicks(srv, 10, func(id int32, data []byte) bool {
		switch id {
		case protocol.IDPlayDamageEvent, protocol.IDPlayHurtAnimation,
			protocol.IDPlayEntityEvent:
			return true
		case protocol.IDPlaySetEntityMotion:
			return cowShoved(data)
		}
		return false
	})
	if got, ok := srv.MobHealth(mob.EntityID); !ok || got != 10 {
		c.T.Errorf("the invulnerable cow's health = %v, %v; want its full 10", got, ok)
	}
}

// TestAnInvulnerableMobTurnsAsideArrowsAndBlasts shoots a survival player's
// arrow at an invulnerable cow and then blasts it: neither lands and the cow
// keeps its health through both, but the blast still shoves it, as vanilla's
// explosion pushes every body in range whatever the hurt does.
func TestAnInvulnerableMobTurnsAsideArrowsAndBlasts(t *testing.T) {
	t.Parallel()
	srv, c, _ := servertest.JoinAs(t, servertest.CombatTestConfig(), "Sniper", servertest.OpsListing(t, "Sniper"))
	c.MoveToGround(2.5, 64, 5.5, true)
	c.Settle()

	c.ChatCommand(`summon minecraft:cow 5.5 64 5.5 {Invulnerable:1b}`)
	mob := c.ExpectMobSpawn(protocol.EntityTypeCow)

	servertest.HoldTicks(t, srv)
	if id := srv.ShootPlayerArrow("Sniper", 2.5, 64.5, 5.5, 10, 0, 0); id == 0 {
		t.Fatal("the arrow never flew")
	}
	c.ExpectNoneWithinTicks(srv, 10, func(id int32, data []byte) bool {
		switch id {
		case protocol.IDPlayDamageEvent, protocol.IDPlayHurtAnimation,
			protocol.IDPlaySetEntityMotion:
			if eid, ok := packetEntityID(data); ok && eid != mob.EntityID {
				return false
			}
			return id != protocol.IDPlaySetEntityMotion || cowShoved(data)
		}
		return false
	})
	if got, ok := srv.MobHealth(mob.EntityID); !ok || got != 10 {
		c.T.Errorf("the invulnerable cow's health after the arrow = %v, %v; want its full 10", got, ok)
	}

	// The tick is stopped so the blast's shove stays the cow's velocity:
	// no tick carries it into a move or decays it before it is read. The
	// velocity it held at the stop is the baseline the shove is read off,
	// so the cow's own wander is not mistaken for the blast's.
	srv.StopTick()
	beforeX, beforeY, beforeZ, ok := srv.MobVelocity(mob.EntityID)
	if !ok {
		t.Fatal("the invulnerable cow is not in the herd")
	}
	srv.BlastAt(5.5, 64, 5.5, 2)
	if got, ok := srv.MobHealth(mob.EntityID); !ok || got != 10 {
		c.T.Errorf("the invulnerable cow's health after the blast = %v, %v; want its full 10", got, ok)
	}
	vx, vy, vz, ok := srv.MobVelocity(mob.EntityID)
	if !ok {
		t.Fatal("the invulnerable cow is not in the herd")
	}
	if dx, dy, dz := vx-beforeX, vy-beforeY, vz-beforeZ; dx*dx+dy*dy+dz*dz == 0 {
		c.T.Errorf("the blast left the invulnerable cow unpushed: velocity = (%v, %v, %v) from (%v, %v, %v)",
			vx, vy, vz, beforeX, beforeY, beforeZ)
	}
}

// TestACreativeSwingStillHurtsAnInvulnerableMob swings a creative player's
// hand at an invulnerable cow: the blow lands, the health coming off under the
// whole hurt sequence a landed hit tells.
func TestACreativeSwingStillHurtsAnInvulnerableMob(t *testing.T) {
	t.Parallel()
	srv, c, attacker := servertest.JoinAs(t, servertest.BlockTestConfig(), "Creator", servertest.OpsListing(t, "Creator"))
	c.MoveToGround(2.5, 64, 5.5, true)
	c.Settle()

	c.ChatCommand(`summon minecraft:cow 5.5 64 5.5 {Invulnerable:1b}`)
	mob := c.ExpectMobSpawn(protocol.EntityTypeCow)

	servertest.AwaitFullSwing(t, srv, "Creator")
	if got := c.Strike(mob.EntityID, attacker); got >= 10 {
		c.T.Errorf("a creative blow left the invulnerable cow at %v, want it hurt", got)
	}
}

// TestKillStillKillsAnInvulnerableMob runs /kill at an invulnerable cow:
// generic_kill is on vanilla's bypasses-invulnerability list, so the cow dies.
func TestKillStillKillsAnInvulnerableMob(t *testing.T) {
	t.Parallel()
	srv, c, _ := servertest.JoinAs(t, servertest.CombatTestConfig(), "Reaper", servertest.OpsListing(t, "Reaper"))
	c.MoveToGround(2.5, 64, 5.5, true)
	c.Settle()

	c.ChatCommand(`summon minecraft:cow 5.5 64 5.5 {Invulnerable:1b}`)
	c.ExpectMobSpawn(protocol.EntityTypeCow)

	c.ChatCommand("kill @e[type=minecraft:cow]")
	servertest.PollUntil(t, 10*time.Second, func() bool { return srv.MobCount() == 0 })
}

// TestAnInvulnerableMobSavesItsFlag stores an invulnerable cow's chunk and
// reads the save back: the flag is written as one.
func TestAnInvulnerableMobSavesItsFlag(t *testing.T) {
	t.Parallel()
	cfg := servertest.MobTestConfig()
	cfg.LevelName = t.TempDir()
	srv, c := servertest.Join(t, cfg, "Keeper", servertest.OpsListing(t, "Keeper"))

	c.ChatCommand(`summon minecraft:cow 1.5 64 0.5 {Invulnerable:1b}`)
	c.ExpectMobSpawn(protocol.EntityTypeCow)

	c.Disconnect()
	servertest.PollUntil(t, 10*time.Second, func() bool { return srv.MobCount() == 0 })
	srv.FlushMobs()

	saved := servertest.SavedMobsByUUID(t, cfg.LevelName)
	if len(saved) != 1 {
		t.Fatalf("the world saved %d mobs, want the cow", len(saved))
	}
	for _, compound := range saved {
		if got := compound["Invulnerable"]; got != nbt.Byte(1) {
			t.Errorf("the saved cow's Invulnerable = %v, want 1b", got)
		}
	}
}

// TestASurvivalPunchLeavesAnInvulnerableStand summons an invulnerable stand
// and punches it: a survival hand's punch is refused with no shake, while a
// hand that turns creative breaks the stand at once.
func TestASurvivalPunchLeavesAnInvulnerableStand(t *testing.T) {
	t.Parallel()
	srv, c := servertest.Join(t, servertest.MobTestConfig(), "Attacker", servertest.OpsListing(t, "Attacker"))

	c.ChatCommand(`summon minecraft:armor_stand 3.5 64 3.5 {Invulnerable:1b}`)
	spawn := c.ExpectArmorStandSpawn()

	c.MoveToGrounded(3.5, 64, 5.5, 180, 40, true)
	c.Settle()
	servertest.AwaitTicks(t, srv, 10)

	servertest.HoldTicks(t, srv)
	c.Attack(spawn.EntityID)
	h := c.HearBlow(spawn.EntityID)
	if len(h.Animations) != 0 {
		t.Errorf("the refused punch played the hurt animation %v", h.Animations)
	}
	if !slices.Equal(h.Sounds, []string{"nodamage"}) {
		t.Errorf("attack sounds of the refused punch = %v, want nodamage alone", h.Sounds)
	}
	c.ExpectNoneWithinTicks(srv, 10, servertest.PacketIs(protocol.IDPlayEntityEvent, protocol.IDPlayRemoveEntities))
	if got := servertest.StandsStanding(srv); got != 1 {
		t.Fatalf("the server holds %d invulnerable stands after a survival punch, want the one", got)
	}

	c.ChatCommand("gamemode creative")
	c.Settle()
	c.Attack(spawn.EntityID)
	c.ExpectRemoval(spawn.EntityID)
	servertest.PollUntil(t, 10*time.Second, func() bool { return servertest.StandsStanding(srv) == 0 })
}

// TestABlastStillShovesAnInvulnerableStand blasts beside an invulnerable
// stand: the blast takes no health, but the stand is shoved all the same, as
// vanilla's explosion pushes every body in range whatever the hurt does.
func TestABlastStillShovesAnInvulnerableStand(t *testing.T) {
	t.Parallel()
	srv, c := servertest.Join(t, servertest.MobTestConfig(), "Blaster", servertest.OpsListing(t, "Blaster"))

	c.ChatCommand(`summon minecraft:armor_stand 3.5 64 3.5 {Invulnerable:1b}`)
	c.ExpectArmorStandSpawn()
	c.MoveToGround(3.5, 64, 5.5, true)
	c.Settle()
	servertest.AwaitTicks(t, srv, 10)

	// The blast off to the +x side, so the shove runs the stand away in -x
	// where the move reads.
	srv.BlastAt(5.5, 64, 3.5, 2)
	servertest.AwaitTicks(t, srv, 20)

	if got := servertest.StandsStanding(srv); got != 1 {
		t.Fatalf("the blast took the invulnerable stand, want it standing")
	}
	stands := srv.ArmorStandsInDimension(world.DimensionName)
	if len(stands) != 1 {
		t.Fatalf("the server holds %d stands, want the one", len(stands))
	}
	if stands[0].X >= 3.5 {
		t.Errorf("the blast left the invulnerable stand at x=%v, want it shoved off 3.5", stands[0].X)
	}
}

// TestAnInvulnerableMobKeepsItsFlagThroughAReload saves an invulnerable cow,
// restarts the server, and swings at the cow it wakes back: the flag rode the
// save and the load, so the survival blow is turned aside still.
func TestAnInvulnerableMobKeepsItsFlagThroughAReload(t *testing.T) {
	t.Parallel()
	cfg := servertest.MobTestConfig()
	cfg.LevelName = t.TempDir()
	srv, c := servertest.Join(t, cfg, "Keeper", servertest.OpsListing(t, "Keeper"))

	c.ChatCommand(`summon minecraft:cow 1.5 64 0.5 {Invulnerable:1b}`)
	c.ExpectMobSpawn(protocol.EntityTypeCow)

	// The save runs while the chunk stands loaded, the way the stand
	// restart test writes one.
	srv.FlushMobs()
	c.Disconnect()
	srv.Shutdown()

	restarted := *cfg
	againSrv, reconnect := servertest.Start(t, &restarted)
	again := reconnect()
	again.Login("Keeper")
	again.Configure()
	back, ok := again.EnterWorldSeeingMob("Keeper", protocol.EntityTypeCow)
	if !ok {
		t.Fatal("the restarted server woke no cow")
	}
	// The mob reader leaves the join before its close: the loading screen
	// is still up, and a player behind it is kept out of harm's way, so
	// the blow below would never be taken. markLoaded closes it.
	again.MarkLoaded()
	againSrv.SetMobNoAI(back.EntityID)

	again.MoveToGround(1.5, 64, 2.5, true)
	again.Settle()
	servertest.AwaitFullSwing(t, againSrv, "Keeper")
	servertest.HoldTicks(t, againSrv)
	again.Attack(back.EntityID)
	again.ExpectNoneWithinTicks(againSrv, 10, func(id int32, data []byte) bool {
		switch id {
		case protocol.IDPlayDamageEvent, protocol.IDPlayHurtAnimation,
			protocol.IDPlayEntityEvent, protocol.IDPlaySetEntityMotion:
			eid, ok := packetEntityID(data)
			return ok && eid == back.EntityID
		}
		return false
	})
	if got, ok := againSrv.MobHealth(back.EntityID); !ok || got != 10 {
		again.T.Errorf("the reloaded cow's health = %v, %v; want its full 10", got, ok)
	}
}

// TestAnInvulnerableStandKeepsItsFlagThroughAReload saves an invulnerable
// stand, restarts the server, and punches the stand it wakes back: the flag
// rode the save and the load, so the survival punch is refused with no shake.
func TestAnInvulnerableStandKeepsItsFlagThroughAReload(t *testing.T) {
	t.Parallel()
	cfg := servertest.SurvivalTestConfig()
	cfg.LevelName = t.TempDir()
	srv, c := servertest.Join(t, cfg, "Keeper", servertest.OpsListing(t, "Keeper"))

	c.ChatCommand(`summon minecraft:armor_stand 1.5 64 1.5 {Invulnerable:1b}`)
	c.ExpectArmorStandSpawn()

	srv.FlushMobs()
	c.Disconnect()
	srv.Shutdown()

	servertest.EntityFileIs(t, cfg.LevelName, world.ChunkPos{X: 0, Z: 0}, "minecraft:armor_stand", func(compound nbt.Compound) {
		if got := compound["Invulnerable"]; got != nbt.Tag(nbt.Byte(1)) {
			t.Errorf("saved Invulnerable = %v, want 1", got)
		}
	})

	restarted := *cfg
	againSrv, reconnect := servertest.Start(t, &restarted)
	again := reconnect()
	again.Login("Keeper")
	again.Configure()
	shown := again.JoinCapturing("Keeper", map[int32]string{
		protocol.EntityTypeArmorStand: "stand",
	})
	back := servertest.CapturedOr(t, again, shown, "stand", protocol.EntityTypeArmorStand)

	again.MoveToGrounded(1.5, 64, 3.5, 180, 40, true)
	again.Settle()
	servertest.AwaitTicks(t, againSrv, 10)
	servertest.HoldTicks(t, againSrv)
	again.Attack(back.EntityID)
	again.ExpectNoneWithinTicks(againSrv, 10, func(id int32, data []byte) bool {
		if id != protocol.IDPlayEntityEvent {
			return false
		}
		eid, ok := packetEntityID(data)
		return ok && eid == back.EntityID
	})
	if got := servertest.StandsStanding(againSrv); got != 1 {
		again.T.Fatalf("the restarted server holds %d invulnerable stands, want the one", got)
	}
}

// TestAnInvulnerableMobTurnsAsideAThrownEgg throws an egg at an invulnerable
// chicken: the egg breaks on it as ever, but no damage event names the
// chicken and its health stands whole.
func TestAnInvulnerableMobTurnsAsideAThrownEgg(t *testing.T) {
	t.Parallel()
	srv, c, _ := servertest.JoinAs(t, servertest.BowTestConfig(), "Thrower")

	chickenID := srv.SpawnMob("minecraft:chicken", 2.5, 64, 0.5)
	c.ExpectMobSpawn(protocol.EntityTypeChicken)
	srv.SetMobInvulnerable(chickenID)
	srv.SetMobSoundVariant(chickenID, "minecraft:classic")
	srv.GiveSlot("Thrower", 36, protocol.ItemStack{ItemID: servertest.MustItem("minecraft:egg"), Count: 16})

	c.MoveTo(0.5, 64, 0.5, -90, 25)
	srv.MoveMob(chickenID, 2.5, 0.5)
	servertest.HoldTicks(t, srv)
	servertest.UseHeld(c, -90, 25)

	traffic := servertest.ReadThrownTraffic(c, srv, 80)
	if egg := traffic.SpawnOfType(protocol.EntityTypeEgg); egg == nil {
		t.Fatal("no egg came into the world for the throw")
	}
	for _, ev := range traffic.DamageEvents {
		if ev.Id == chickenID {
			t.Errorf("the invulnerable chicken was named in a damage event (type %d)", ev.DamageType)
		}
	}
	if got, ok := srv.MobHealth(chickenID); !ok || got != 4 {
		t.Errorf("the invulnerable chicken's health = %v, %v; want its full 4", got, ok)
	}
}

// TestASweepTurnsAsideAnInvulnerableBystander plants an invulnerable cow
// beside the stricken one and swings a full sword: the bystander hears
// nothing of the sweep — no damage event, no hurt status, no shove — while
// the stricken cow takes the blade's blow.
func TestASweepTurnsAsideAnInvulnerableBystander(t *testing.T) {
	t.Parallel()
	f := newSweepFixture(t)
	f.watcher.MoveToGround(40.5, 64, 40.5, true)
	f.watcher.Settle()
	f.ironSword(t)

	f.srv.StopTick()
	stricken := f.srv.SpawnMob("minecraft:cow", 4.5, 64, 5.5)
	beside := f.srv.SpawnMob("minecraft:cow", 5.45, 64, 5.5)
	f.srv.SetMobInvulnerable(beside)
	f.fencer.Settle()
	f.watcher.Settle()

	f.fencer.Attack(stricken)
	f.fencer.Settle()
	heard := f.watcher.HearBodies()
	if got := heard.DamageEvents[stricken]; got != f.fencerID+1 {
		t.Errorf("the blow on the stricken cow named %d as the cause, want the fencer's %d", got, f.fencerID+1)
	}
	if heard.DamageEvents[beside] != 0 {
		t.Error("a damage event named the invulnerable bystander")
	}
	if heard.HurtEvents[beside] {
		t.Error("the invulnerable bystander took a hurt status")
	}
	if heard.Motions[beside] != nil {
		t.Error("the invulnerable bystander was shoved")
	}
	if got := heard.Healths[beside]; got != 0 && got != 10 {
		t.Errorf("the invulnerable bystander was left at %v, want its full 10", got)
	}
}

// TestAnInvulnerableMobTurnsAsideAFireworkBurst launches a one-explosion
// rocket under a stone ceiling so its burst falls beside an invulnerable
// cow: the burst sends no damage event and the cow keeps its health, the
// rocket's own burst and removal riding as ever.
func TestAnInvulnerableMobTurnsAsideAFireworkBurst(t *testing.T) {
	t.Parallel()
	srv, c, _ := servertest.JoinAs(t, servertest.BowTestConfig(), "Launcher")

	stone, ok := registry.BlockStates["minecraft:stone"]
	if !ok {
		t.Fatal("the block registry has no minecraft:stone")
	}
	srv.SetBlock(5, 65, 2, stone)
	cowID := srv.SpawnMob("minecraft:cow", 5.5, 64, 5.5)
	srv.SetMobNoAI(cowID)
	srv.SetMobInvulnerable(cowID)

	srv.GiveSlot("Launcher", 36, servertest.FireworkStack(0, 1))
	c.MoveTo(3.5, 64, 1.5, -90, -25)
	servertest.HoldTicks(t, srv)
	useItemOnFace(c, 5, 63, 2, protocol.FaceTop, 0.5, 0.9, 0.5)

	traffic := readFireworkTraffic(c, srv, 60)
	if traffic.fireworkOf() == nil {
		t.Fatal("no firework rocket came into the world for the use")
	}
	for _, ev := range traffic.damageEvents {
		if ev.Id == cowID {
			t.Errorf("the invulnerable cow was named in a damage event (type %d)", ev.DamageType)
		}
	}
	if got, ok := srv.MobHealth(cowID); !ok || got != 10 {
		t.Errorf("the invulnerable cow's health after the burst = %v, %v; want its full 10", got, ok)
	}
}

// TestACreativeArrowStillHurtsAnInvulnerableMob turns the shooter creative
// and looses an arrow at an invulnerable cow: a creative source is on the
// bypass list, so the arrow lands and the cow's health comes off.
func TestACreativeArrowStillHurtsAnInvulnerableMob(t *testing.T) {
	t.Parallel()
	srv, c, _ := servertest.JoinAs(t, servertest.CombatTestConfig(), "Sniper", servertest.OpsListing(t, "Sniper"))
	c.MoveToGround(2.5, 64, 5.5, true)
	c.Settle()

	// Without AI from the summon on: a cow left to wander for even a few
	// ticks can step off the arrow's line.
	c.ChatCommand(`summon minecraft:cow 5.5 64 5.5 {Invulnerable:1b,NoAI:1b}`)
	mob := c.ExpectMobSpawn(protocol.EntityTypeCow)

	c.ChatCommand("gamemode creative")
	servertest.PollUntil(t, 10*time.Second, func() bool { return srv.PlayerGameMode("Sniper") == protocol.GameModeCreative })

	// Driven by hand: the arrow's twenty kills the cow, which leaves the herd
	// on the next tick, so its hurt health stands for one tick alone.
	srv.StopTick()
	if id := srv.ShootPlayerArrow("Sniper", 2.5, 64.5, 5.5, 10, 0, 0); id == 0 {
		t.Fatal("the arrow never flew")
	}
	got, ok := srv.MobHealth(mob.EntityID)
	for range 20 {
		srv.RunTick()
		got, ok = srv.MobHealth(mob.EntityID)
		if !ok || got < 10 {
			break
		}
	}
	if !ok || got >= 10 {
		t.Errorf("a creative arrow left the invulnerable cow at %v, %v; want it hurt", got, ok)
	}
}

// TestACreativeBlastStillHurtsAnInvulnerableMob fires a creative player's
// blast at an invulnerable cow: the creative source is on the bypass list,
// so the blast lands and the cow's health comes off.
func TestACreativeBlastStillHurtsAnInvulnerableMob(t *testing.T) {
	t.Parallel()
	srv, c, _ := servertest.JoinAs(t, servertest.CombatTestConfig(), "Blaster", servertest.OpsListing(t, "Blaster"))
	c.MoveToGround(2.5, 64, 5.5, true)
	c.Settle()

	c.ChatCommand(`summon minecraft:cow 5.5 64 5.5 {Invulnerable:1b}`)
	mob := c.ExpectMobSpawn(protocol.EntityTypeCow)
	c.ChatCommand("gamemode creative")
	servertest.PollUntil(t, 10*time.Second, func() bool { return srv.PlayerGameMode("Blaster") == protocol.GameModeCreative })

	srv.StopTick()
	srv.BlastAtCause(5.5, 64, 5.5, 2, "Blaster")
	if got, ok := srv.MobHealth(mob.EntityID); !ok || got >= 10 {
		t.Errorf("a creative blast left the invulnerable cow at %v, %v; want it hurt", got, ok)
	}
}
