package entities

import (
	"strings"
	"testing"

	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/internal/server/servertest"
	"github.com/golem-mc/golem/nbt"
)

// vanilla: SummonCommand.createEntity, ZombieHorse.createAttributes,
// AbstractHorse.createBaseHorseAttributes, SkeletonHorse.addAdditionalSaveData
func TestSummonedZombieHorseDefaults(t *testing.T) {
	t.Parallel()
	srv, c := servertest.Join(t, livingSavedConfig(), "Rider", servertest.OpsListing(t, "Rider"))
	servertest.HoldTicks(t, srv)
	c.ChatCommand("summon zombie_horse ~ ~ ~3 {NoAI:1b}")
	c.Settle()

	c.ChatCommand("attribute @e[type=zombie_horse,limit=1] minecraft:movement_speed base get")
	feedback, ok := protocol.ReadText(protocol.NewReader(c.NextSystemChat()))
	if !ok || feedback.Translate != "commands.attribute.base_value.get.success" || len(feedback.With) != 3 {
		t.Fatalf("attribute feedback = %+v", feedback)
	}
	// The attribute starts as 0.225f, widened to a double for the %s feedback.
	if got := feedback.With[2].Text; got != "0.22499999403953552" {
		t.Errorf("default movement speed = %s, want 0.22499999403953552", got)
	}

	data := livingData(t, c, "@e[type=zombie_horse,limit=1]", "")
	tag, err := nbt.ParseSNBT(data)
	if err != nil {
		t.Fatal(err)
	}
	attributes, _ := nbt.Get[nbt.List](tag, "attributes")
	if len(attributes.Items) != 1 {
		t.Errorf("saved attributes = %v, want only movement_speed", attributes)
	} else if entry, _ := nbt.CompoundView(attributes.Items[0]); entry["id"] != nbt.String("minecraft:movement_speed") {
		t.Errorf("saved attribute = %v, want movement_speed", entry)
	}
	if strings.Contains(data, "SkeletonTrap") {
		t.Errorf("zombie horse saved skeleton trap fields: %s", data)
	}
	c.ChatCommand("data get entity @e[type=zombie_horse,nbt={SkeletonTrap:0b},limit=1]")
	servertest.ExpectCommandError(t, c, "argument.entity.notfound.entity")

	c.ChatCommand("summon skeleton_horse ~ ~ ~ {NoAI:1b}")
	c.Settle()
	if got := livingData(t, c, "@e[type=skeleton_horse,limit=1]", "SkeletonTrap"); got != "0b" {
		t.Errorf("skeleton horse SkeletonTrap = %s, want 0b", got)
	}
	if got := livingData(t, c, "@e[type=skeleton_horse,limit=1]", "SkeletonTrapTime"); got != "0" {
		t.Errorf("skeleton horse SkeletonTrapTime = %s, want 0", got)
	}
}
