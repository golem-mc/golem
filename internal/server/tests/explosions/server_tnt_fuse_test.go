package explosions

import (
	"testing"
	"time"

	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/internal/server"
	"github.com/golem-mc/golem/internal/server/servertest"
	"github.com/golem-mc/golem/internal/server/world"
	"github.com/golem-mc/golem/nbt"
)

// A primed TNT's stored fuse is kept as read, nought and below included, and
// only an absent one is the default eighty; a fuse at or under nought goes
// off on the body's first tick.
// vanilla: PrimedTnt.readAdditionalSaveData, PrimedTnt.tick

// summonTNTOnStepClock joins an operator on a step clock and summons a
// primed TNT with the given compound, returning once the body stands.
func summonTNTOnStepClock(t *testing.T, tag string) (*server.Server, *servertest.StepClock, *servertest.Client) {
	t.Helper()
	clock := servertest.NewStepClock()
	srv, c := servertest.Join(t, servertest.MobTestConfig(), "Keeper", servertest.OpsListing(t, "Keeper"), func(s *server.Server) {
		s.SetTickClock(clock, nil)
		s.SetKeepAlive(time.Hour, time.Hour)
	})
	c.ChatCommand("summon minecraft:tnt 6.5 64 6.5" + tag)
	c.AwaitSpawn(protocol.EntityTypeTnt)
	if n := len(srv.TNTsInDimension(world.DimensionName)); n != 1 {
		t.Fatalf("primed TNT burning after the summon = %d, want 1", n)
	}
	return srv, clock, c
}

func TestASummonedTNTWithFuseZeroExplodesOnItsFirstTick(t *testing.T) {
	t.Parallel()
	for _, tag := range []string{" {fuse:0}", " {fuse:-5s}"} {
		t.Run(tag, func(t *testing.T) {
			t.Parallel()
			srv, clock, c := summonTNTOnStepClock(t, tag)
			servertest.StepTicks(t, srv, clock, 1)
			if tnts := srv.TNTsInDimension(world.DimensionName); len(tnts) != 0 {
				t.Fatalf("after one tick the TNT still burns with fuse %d, want it gone off", tnts[0].Fuse)
			}
			c.AwaitPlay(protocol.IDPlayExplosion)
		})
	}
}

func TestASummonedTNTWithoutAFuseBurnsEightyTicks(t *testing.T) {
	t.Parallel()
	srv, clock, c := summonTNTOnStepClock(t, "")
	servertest.StepTicks(t, srv, clock, 79)
	tnts := srv.TNTsInDimension(world.DimensionName)
	if len(tnts) != 1 || tnts[0].Fuse != 1 {
		t.Fatalf("after 79 ticks TNT = %+v, want one burning with fuse 1", tnts)
	}
	servertest.StepTicks(t, srv, clock, 1)
	if n := len(srv.TNTsInDimension(world.DimensionName)); n != 0 {
		t.Fatalf("after 80 ticks %d TNT still burn, want it gone off", n)
	}
	c.AwaitPlay(protocol.IDPlayExplosion)
}

func TestASavedTNTWithFuseZeroExplodesOnItsFirstTickAfterARestart(t *testing.T) {
	t.Parallel()
	cfg := servertest.SurvivalConfig()
	cfg.LevelName = t.TempDir()

	// No tick runs before the save, so the body is written with the fuse
	// of nought it was lit with.
	clock := servertest.NewStepClock()
	srv, c := servertest.Join(t, cfg, "Keeper", func(s *server.Server) {
		s.SetTickClock(clock, nil)
		s.SetKeepAlive(time.Hour, time.Hour)
	})
	srv.SpawnTNT(20.5, 64, 20.5, 0)
	c.AwaitSpawn(protocol.EntityTypeTnt)
	srv.FlushMobs()
	srv.Shutdown()

	servertest.EntityFileIs(t, cfg.LevelName, world.ChunkPos{X: 1, Z: 1}, "minecraft:tnt", func(compound nbt.Compound) {
		if fuse, ok := nbt.Get[nbt.Short](compound, "fuse"); !ok || fuse != 0 {
			t.Fatalf("saved fuse = %d (present %v), want 0", fuse, ok)
		}
	})

	restarted := *cfg
	clock2 := servertest.NewStepClock()
	srv2, again := servertest.Join(t, &restarted, "Keeper", func(s *server.Server) {
		s.SetTickClock(clock2, nil)
		s.SetKeepAlive(time.Hour, time.Hour)
	})
	// The saved chunk's entities wake on a tick: the body is either gone off
	// on the tick it woke or seen still at nought, then gone on the next.
	for range 20 {
		servertest.StepTicks(t, srv2, clock2, 1)
		tnts := srv2.TNTsInDimension(world.DimensionName)
		if len(tnts) == 0 {
			continue
		}
		if tnts[0].Fuse > 0 {
			t.Fatalf("the restored TNT burns with fuse %d, want the 0 it was saved with", tnts[0].Fuse)
		}
		servertest.StepTicks(t, srv2, clock2, 1)
		if n := len(srv2.TNTsInDimension(world.DimensionName)); n != 0 {
			t.Fatalf("the restored TNT still burns after its first tick")
		}
		break
	}
	again.AwaitPlay(protocol.IDPlayExplosion)
}

// TestASilentSummonedTNTBlastsWithoutItsSound checks the Explosion packet's
// playSound flag follows the source's Silent tag.
// vanilla: ServerLevel.explode
func TestASilentSummonedTNTBlastsWithoutItsSound(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		tag  string
		want bool
	}{{" {fuse:0}", true}, {" {fuse:0,Silent:1b}", false}} {
		t.Run(tc.tag, func(t *testing.T) {
			t.Parallel()
			srv, clock, c := summonTNTOnStepClock(t, tc.tag)
			servertest.StepTicks(t, srv, clock, 1)
			boom := servertest.ReadExplosion(c, c.AwaitPlay(protocol.IDPlayExplosion))
			if boom.PlaySound != tc.want {
				t.Errorf("playSound = %v, want %v", boom.PlaySound, tc.want)
			}
		})
	}
}

// TestASilentSummonedBlastSourceTellsItsSilentFlag checks Silent reaches the
// client as the spawn's metadata on primed TNT and an end crystal.
// vanilla: Entity.DATA_SILENT
func TestASilentSummonedBlastSourceTellsItsSilentFlag(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name string
		kind int32
		tag  string
	}{
		{"minecraft:tnt", protocol.EntityTypeTnt, "{fuse:80,Silent:1b}"},
		{"minecraft:end_crystal", protocol.EntityTypeEndCrystal, "{Silent:1b}"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			c := servertest.DataWorld(t, "Hush")
			c.ChatCommand("summon " + tc.name + " 4.5 70 4.5 " + tc.tag)
			spawn := c.AwaitSpawn(tc.kind)
			if m := servertest.MetaOf(c.AwaitMetadataFor(spawn.EntityID), protocol.MetadataIndexSilent); m == nil || !m.FlagVal {
				t.Fatalf("the spawn's silent metadata = %+v, want the flag set", m)
			}
		})
	}
}
