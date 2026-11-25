package sleep

import (
	"slices"
	"testing"

	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/internal/server/servertest"
	"github.com/golem-mc/golem/nbt"
)

// vanilla: AbstractBedBlock.useWithoutItem, LivingEntity.addAdditionalSaveData
func TestSleepingPlayerDataNamesTheBedHeadUntilWake(t *testing.T) {
	t.Parallel()
	const name = "Actor"
	cfg := servertest.SurvivalConfig()
	cfg.LevelName = t.TempDir()
	file := servertest.PlayerFile(cfg.LevelName, name)
	// A rejoining player starts awake even if their previous save was taken in bed.
	servertest.WritePlayerFile(t, file, nbt.Compound{"sleeping_pos": nbt.IntArray{3, 64, 2}})
	srv, c := servertest.Join(t, cfg, name, servertest.OpsListing(t, name))
	servertest.HoldTicks(t, srv)
	missingPos := servertest.CommandErrorMessageForTest("arguments.nbtpath.nothing_found", "sleeping_pos")
	c.ChatCommand("data get entity @s sleeping_pos")
	c.ExpectCommandFailure(missingPos)

	servertest.StandSleepBed(t, srv)
	c.ChatCommand("time set 14000")
	c.ExpectUpdateTime(14000)
	c.StandAt(6.5, 64, 0.5, 0)
	c.UseBlock(sleepBedHeadX, sleepBedHeadY, sleepBedFootZ)
	c.Settle()
	if !srv.Sleeping(name) {
		t.Fatal("the bed click did not put the player to sleep")
	}

	c.ChatCommand("execute if data entity @s sleeping_pos")
	c.ExpectCommandFeedbackText("commands.execute.conditional.pass_count", protocol.Text{Number: new(int64(1))})
	c.ChatCommand("data get entity @s sleeping_pos")
	c.ExpectCommandFeedbackText("commands.data.entity.query",
		servertest.PlayerDisplayComponent(name), servertest.NBTArg("[I; 5, 64, 1]"))

	srv.SaveAll(false)
	root := servertest.ReadPlayerFile(t, file)
	want := nbt.IntArray{sleepBedHeadX, sleepBedHeadY, sleepBedHeadZ}
	if got, _ := nbt.Get[nbt.IntArray](root, "sleeping_pos"); !slices.Equal(got, want) {
		t.Errorf("saved sleeping_pos = %v, want %v", got, want)
	}

	c.LeaveBed()
	c.Settle()
	c.ChatCommand("execute if data entity @s sleeping_pos")
	c.ExpectCommandFailure(servertest.CommandErrorMessageForTest("commands.execute.conditional.fail"))
	c.ChatCommand("data get entity @s sleeping_pos")
	c.ExpectCommandFailure(missingPos)

	srv.SaveAll(false)
	if tag := servertest.ReadPlayerFile(t, file)["sleeping_pos"]; tag != nil {
		t.Errorf("awake player still has saved sleeping_pos = %v", tag)
	}
}
