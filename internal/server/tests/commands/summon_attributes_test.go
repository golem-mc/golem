package commands

import (
	"fmt"
	"testing"

	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/internal/server/servertest"
)

func TestSummonAttributesReadsUnqualifiedIdAndIntBase(t *testing.T) {
	t.Parallel()
	srv, c := servertest.Join(t, servertest.MobTestConfig(), "Tester", servertest.OpsListing(t, "Tester"))
	servertest.HoldTicks(t, srv)
	for i, tc := range []struct {
		entry, attribute, want string
	}{
		{`{id:"max_health",base:100}`, "max_health", "100.0"},
		{`{id:"minecraft:armor",base:5b}`, "armor", "5.0"},
		{`{id:"max_health",base:100d}`, "max_health", "100.0"},
		{`{id:"minecraft:max_health",base:100}`, "max_health", "100.0"},
		{`{id:"armor",base:5s}`, "armor", "5.0"},
		{`{id:"armor",base:5L}`, "armor", "5.0"},
		{`{id:"armor",base:5f}`, "armor", "5.0"},
		{`{id:"armor",base:5d,modifiers:[{id:"minecraft:bonus",amount:2,operation:"add_value"}]}`, "armor", "7.0"},
		{`{id:"max_health",base:20b,modifiers:[{id:"bonus",amount:80s,operation:"add_value"}]}`, "max_health", "100.0"},
	} {
		t.Run(fmt.Sprint(i), func(t *testing.T) {
			c.ChatCommand(fmt.Sprintf("summon zombie ~ ~ ~2 {NoAI:1b,Health:100f,attributes:[%s]}", tc.entry))
			mob := c.ExpectMobSpawn(protocol.EntityTypeZombie)
			who := servertest.EntityDisplayComponent("minecraft:zombie", "entity.minecraft.zombie", mob.Uuid)
			servertest.AwaitCommandFeedbackText(t, c, "commands.summon.success", who)
			selector := mob.Uuid.String()
			if i == 0 {
				selector = "@e[type=zombie,limit=1]"
			}
			c.ChatCommand(fmt.Sprintf("attribute %s %s get", selector, tc.attribute))
			value := ""
			c.SettleAndCollect(func(id int32, data []byte) {
				if id != protocol.IDPlaySystemChat {
					return
				}
				if text, ok := protocol.ReadText(protocol.NewReader(data)); ok && text.Translate == "commands.attribute.value.get.success" {
					value = text.With[len(text.With)-1].Text
				}
			})
			if value != tc.want {
				t.Errorf("%s: %s = %q, want %s", tc.entry, tc.attribute, value, tc.want)
			}
			if tc.attribute == "max_health" {
				c.ChatCommand("data get entity " + selector + " Health")
				servertest.AwaitCommandFeedbackText(t, c, "commands.data.entity.query", who, servertest.NBTArg("100.0f"))
				base := "100.0d"
				if i == 8 {
					base = "20.0d"
				}
				c.ChatCommand("data get entity " + selector + ` attributes[{id:"minecraft:max_health"}].base`)
				servertest.AwaitCommandFeedbackText(t, c, "commands.data.entity.query", who, servertest.NBTArg(base))
			}
		})
	}
}

func TestDataMergeAttributesReadsUnqualifiedIdAndNumericTags(t *testing.T) {
	t.Parallel()
	srv, c := servertest.Join(t, servertest.MobTestConfig(), "Tester", servertest.OpsListing(t, "Tester"))
	servertest.HoldTicks(t, srv)
	c.ChatCommand("summon zombie ~ ~ ~2 {NoAI:1b}")
	mob := c.ExpectMobSpawn(protocol.EntityTypeZombie)
	who := servertest.EntityDisplayComponent("minecraft:zombie", "entity.minecraft.zombie", mob.Uuid)
	servertest.AwaitCommandFeedbackText(t, c, "commands.summon.success", who)
	target := mob.Uuid.String()
	c.ChatCommand("data merge entity " + target + ` {Health:105f,attributes:[{id:"max_health",base:100s,modifiers:[{id:"bonus",amount:5b,operation:"add_value"}]}]}`)
	servertest.AwaitSystemChatSaying(t, c, "commands.data.entity.modified")
	c.ChatCommand("attribute " + target + " max_health get")
	servertest.AwaitCommandFeedbackText(t, c, "commands.attribute.value.get.success",
		protocol.Text{Translate: "attribute.name.max_health"}, who, protocol.Text{Text: "105.0"})
	c.ChatCommand("data get entity " + target + " Health")
	servertest.AwaitCommandFeedbackText(t, c, "commands.data.entity.query", who, servertest.NBTArg("105.0f"))
	c.ChatCommand("data get entity " + target + ` attributes[{id:"minecraft:max_health"}].modifiers[{id:"minecraft:bonus"}].amount`)
	servertest.AwaitCommandFeedbackText(t, c, "commands.data.entity.query", who, servertest.NBTArg("5.0d"))
}
