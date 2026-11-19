package entities

import (
	"testing"

	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/internal/server/servertest"
	"github.com/golem-mc/golem/internal/server/world"
	"github.com/golem-mc/golem/nbt"
	"github.com/golem-mc/golem/registry"
)

// vanilla: Projectile.readAdditionalSaveData, Projectile.addAdditionalSaveData
func TestArrowLeftOwnerNBTWritesAndRestart(t *testing.T) {
	t.Parallel()
	cfg := servertest.BlockTestConfig()
	cfg.LevelName = t.TempDir()
	srv, c := servertest.Join(t, cfg, "Keeper", servertest.OpsListing(t, "Keeper"))
	servertest.HoldTicks(t, srv)
	kinds := []string{"arrow", "spectral_arrow", "trident"}
	for _, kind := range kinds {
		target := "@e[type=minecraft:" + kind + ",limit=1]"
		c.ChatCommand("summon minecraft:" + kind + " 4.5 70 4.5 {NoGravity:1b,Owner:[I;1,2,3,4],LeftOwner:1b}")
		typ, _ := registry.EntityTypeID("minecraft:" + kind)
		spawn := c.AwaitSpawn(typ)
		name := servertest.EntityDisplayComponent("minecraft:"+kind, "entity.minecraft."+kind, spawn.Uuid)
		c.ExpectCommandFeedbackText("commands.summon.success", name)
		expectArrowLeftOwner(t, c, target, true)
		for _, step := range []struct {
			command string
			left    bool
		}{
			{"data merge entity " + target + " {LeftOwner:0b}", false},
			{"data merge entity " + target + " {LeftOwner:1b}", true},
			{"data remove entity " + target + " LeftOwner", false},
			{"data modify entity " + target + " LeftOwner set value 1b", true},
			{"data merge entity " + target + " {Tags:[kept]}", true},
		} {
			c.ChatCommand(step.command)
			c.ExpectCommandFeedbackText("commands.data.entity.modified", name)
			expectArrowLeftOwner(t, c, target, step.left)
		}
	}
	srv.FlushMobs()
	for _, kind := range kinds {
		servertest.EntityFileIs(t, cfg.LevelName, world.ChunkPos{}, "minecraft:"+kind, func(tag nbt.Compound) {
			if got := tag["LeftOwner"]; got != nbt.Byte(1) {
				t.Errorf("saved %s LeftOwner = %v, want 1b", kind, got)
			}
		})
	}
	c.Disconnect()
	srv.Shutdown()

	restarted := *cfg
	againSrv, connect := servertest.Start(t, &restarted, servertest.OpsListing(t, "Keeper"))
	servertest.FreezeUntilHeld(t, againSrv)
	again := connect().Join("Keeper")
	servertest.HoldTicks(t, againSrv)
	// Wake the saved entities through the mob pass alone: an arrow tick
	// would set LeftOwner even if the NBT reader had discarded it.
	againSrv.TickMobs(1)
	for _, kind := range kinds {
		expectArrowLeftOwner(t, again, "@e[type=minecraft:"+kind+",limit=1]", true)
	}
}

func expectArrowLeftOwner(t *testing.T, c *servertest.Client, target string, want bool) {
	t.Helper()
	c.ChatCommand("data get entity " + target)
	text, ok := protocol.ReadText(protocol.NewReader(c.ExpectPlay(protocol.IDPlaySystemChat)))
	if !ok || text.Translate != "commands.data.entity.query" || len(text.With) != 2 {
		t.Fatalf("%s query feedback = %+v, want entity data", target, text)
	}
	tag, err := nbt.ParseSNBT(text.With[1].Plain())
	if err != nil {
		t.Fatal(err)
	}
	got, present := tag["LeftOwner"]
	if want && got != nbt.Byte(1) || !want && present {
		t.Fatalf("%s LeftOwner = %v, want true saved as 1b or false omitted (want %t)", target, got, want)
	}
	if got := tag["HasBeenShot"]; got != nbt.Byte(0) {
		t.Fatalf("%s HasBeenShot = %v, want 0b without projectile physics", target, got)
	}
}
