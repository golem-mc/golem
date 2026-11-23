package sleep

import (
	"fmt"
	"testing"

	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/internal/server/servertest"
	"github.com/golem-mc/golem/registry"
)

// The straw bed reads gameplay/straw_bed_rule: DESTROY_ON_LEAVE in the
// overworld (sleep by night, no spawn point, gone on waking) and
// DESTROY_ON_USE in the nether (gone on the click, no explosion).
// vanilla: StrawBedBlock, AbstractBedBlock.useWithoutItem, BedRule

const strawBreak = "minecraft:block.straw_bed.break_leave"

// awaitStrawBreak reads until the straw bed's break sound and returns where
// it played, in the wire's eighths, failing unless it is a block sound.
func awaitStrawBreak(t *testing.T, c *servertest.Client) (x, y, z int32) {
	t.Helper()
	for id, data := range c.Packets(servertest.ExpectTimeout) {
		if id != protocol.IDPlaySoundEffect {
			continue
		}
		sound, category, sx, sy, sz, _, _ := servertest.ReadAmbient(t, data)
		if sound != strawBreak {
			continue
		}
		if category != protocol.SoundCategoryBlock {
			t.Errorf("the break sound played in category %d, want %d (block)", category, protocol.SoundCategoryBlock)
		}
		return sx, sy, sz
	}
	t.Fatalf("no %s within %s", strawBreak, servertest.ExpectTimeout)
	return 0, 0, 0
}

func strawBedState(t *testing.T, part string) uint16 {
	t.Helper()
	return servertest.MustState(t, "minecraft:straw_bed", map[string]string{"part": part, "facing": "south"})
}

func TestSleepingPlayerSpawnsAtBedHeight(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		bed     string
		offsetY float64
	}{
		{"straw_bed", 0.375},
		{"red_bed", 0.6875},
	} {
		t.Run(tc.bed, func(t *testing.T) {
			srv, sleeper := servertest.Join(t, servertest.BlockTestConfig(), "Actor", servertest.OpsListing(t, "Actor"))
			servertest.HoldTicks(t, srv)
			srv.SetGameRuleValue("players_sleeping_percentage", 101)
			sleeper.ChatCommand(fmt.Sprintf("setblock %d %d %d %s[facing=south,part=foot] strict", sleepBedHeadX, sleepBedHeadY, sleepBedFootZ, tc.bed))
			sleeper.ChatCommand(fmt.Sprintf("setblock %d %d %d %s[facing=south,part=head] strict", sleepBedHeadX, sleepBedHeadY, sleepBedHeadZ, tc.bed))
			sleeper.ChatCommand("time set 13000")
			sleeper.MoveTo(6.5, 64, 0.5, 0, 0)
			sleeper.UseBlock(sleepBedHeadX, sleepBedHeadY, sleepBedHeadZ)
			sleeper.Settle()
			if !srv.Sleeping("Actor") {
				t.Fatal("the bed did not put the player to sleep")
			}

			// A new viewer receives the absolute position in Spawn Entity.
			servertest.ReleaseTicks(t, srv)
			watcher := servertest.DialServer(t, srv).JoinKeeping("Watcher")
			if !watcher.ReadTicks(srv, 5, func(id int32, data []byte) bool {
				if id != protocol.IDPlaySpawnEntity {
					return false
				}
				spawn := servertest.ReadThrownSpawn(watcher, data)
				if spawn.EntityID != sleeper.PlayerEntityID {
					return false
				}
				if wantY := sleepBedHeadY + tc.offsetY; spawn.X != 5.5 || spawn.Y != wantY || spawn.Z != 1.5 {
					t.Errorf("sleeper spawn position = (%v, %v, %v), want (5.5, %v, 1.5)", spawn.X, spawn.Y, spawn.Z, wantY)
				}
				return true
			}) {
				t.Fatal("the watcher did not receive the sleeper's spawn packet")
			}
		})
	}
}

// TestAStrawBedSleepsWithoutASpawnAndBreaksOnWaking sleeps in an overworld
// straw bed at night: the sleep starts, no spawn point is set, the straw bed
// stat counts it and slept_in_bed does not fire, and the wake takes both
// halves with the break sound.
func TestAStrawBedSleepsWithoutASpawnAndBreaksOnWaking(t *testing.T) {
	t.Parallel()
	srv, c := servertest.Join(t, servertest.BlockTestConfig(), "Actor", servertest.NoTimeSync)
	srv.StopTick()
	if !srv.SetBlock(sleepBedHeadX, sleepBedHeadY, sleepBedHeadZ, strawBedState(t, "head")) ||
		!srv.SetBlock(sleepBedHeadX, sleepBedHeadY, sleepBedFootZ, strawBedState(t, "foot")) {
		t.Fatal("could not stand the straw bed")
	}
	servertest.SetNight(t, srv)
	c.MoveTo(6.5, 64, 0.5, 0, 0)
	c.Settle()

	c.UseBlock(sleepBedHeadX, sleepBedHeadY, sleepBedHeadZ)
	c.Settle()
	if !srv.Sleeping("Actor") {
		t.Fatal("the straw bed did not put the player to sleep")
	}
	if _, _, ok := srv.SpawnPointOf("Actor"); ok {
		t.Error("the straw bed set a spawn point")
	}
	if got := srv.StatValue("Actor", registry.StatCustom, "minecraft:sleep_in_straw_bed"); got != 1 {
		t.Errorf("sleep_in_straw_bed = %d, want 1", got)
	}
	if got := srv.StatValue("Actor", registry.StatCustom, "minecraft:sleep_in_bed"); got != 0 {
		t.Errorf("sleep_in_bed = %d, want 0", got)
	}
	if srv.AdvancementObtained("Actor", "minecraft:adventure/sleep_in_bed", "slept_in_bed") {
		t.Error("a straw bed, which sets no spawn, fired slept_in_bed")
	}

	c.LeaveBed()
	srv.StartTick()
	x, y, z := awaitStrawBreak(t, c)
	footEvents := map[int32]bool{}
	c.ReadTicks(srv, 5, func(id int32, data []byte) bool {
		if id == protocol.IDPlayWorldEvent {
			ev, ex, ey, ez, _ := servertest.ReadWorldEvent(t, data)
			if ex == sleepBedHeadX && ey == sleepBedHeadY && ez == sleepBedFootZ {
				footEvents[ev] = true
			}
		}
		return false
	})
	if x != sleepBedHeadX*8+4 || y != sleepBedHeadY*8+4 || z != sleepBedHeadZ*8+4 {
		t.Errorf("the break sound played at eighths (%d,%d,%d), want the head's centre", x, y, z)
	}
	c.Settle()
	// The foot goes through destroyBlock: particles only, no second sound.
	if !footEvents[protocol.WorldEventDestroyBlockParticles] || footEvents[protocol.WorldEventBlockBreak] {
		t.Errorf("foot level events = %v, want only 2014", footEvents)
	}
	if srv.Sleeping("Actor") {
		t.Error("the leave bed left the player asleep")
	}
	for _, cz := range []int32{sleepBedHeadZ, sleepBedFootZ} {
		if state := srv.BlockAt(sleepBedHeadX, sleepBedHeadY, cz); state != 0 {
			name, _ := servertest.StateProps(state)
			t.Errorf("the straw bed's half at z=%d survived the wake: %q", cz, name)
		}
	}
}
