package entities

import (
	"os"
	"reflect"
	"strings"
	"testing"

	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/internal/server/servertest"
	"github.com/golem-mc/golem/nbt"
)

// vanilla: AttributeMap.pack, Rabbit.setVariant, LivingEntity.collectEquipmentChanges
func TestMobSavesDefaultValuedAttributeInstances(t *testing.T) {
	t.Parallel()
	cfg := livingSavedConfig()
	srv, c := servertest.Join(t, cfg, "Rider", servertest.OpsListing(t, "Rider"))
	servertest.HoldTicks(t, srv)
	c.ChatCommand(`summon rabbit ~ ~ ~3 {NoAI:1b,NoGravity:1b,Tags:["r"]}`)
	servertest.AwaitSystemChatSaying(t, c, "commands.summon.success")
	c.ChatCommand(`summon pig ~ ~ ~3 {NoAI:1b,NoGravity:1b,Tags:["p"],attributes:[{id:"minecraft:scale",base:2d}],equipment:{chest:{id:"diamond_chestplate",count:1}}}`)
	servertest.AwaitSystemChatSaying(t, c, "commands.summon.success")
	checkMobAttributeFixtures(t, c, "loaded")
	for _, tag := range []string{"r", "p"} {
		c.ChatCommand("data merge entity @e[tag=" + tag + ",limit=1] {Health:2f}")
		servertest.AwaitSystemChatSaying(t, c, "commands.data.entity.modified")
	}
	checkMobAttributeFixtures(t, c, "merged")
	// The equipment's instances come with the first tick, not the command.
	servertest.RunTicks(t, srv, 1)
	c.Settle()
	checkMobAttributeFixtures(t, c, "ticked")
	for _, tag := range []string{"r", "p"} {
		c.ChatCommand("data merge entity @e[tag=" + tag + ",limit=1] {Health:1f}")
		servertest.AwaitSystemChatSaying(t, c, "commands.data.entity.modified")
	}
	checkMobAttributeFixtures(t, c, "ticked-merged")
	// vanilla: Rabbit.setVariant keeps the evil attack bonus transient.
	c.ChatCommand(`summon rabbit ~ ~ ~3 {NoAI:1b,NoGravity:1b,RabbitType:99,Tags:["k"]}`)
	servertest.AwaitSystemChatSaying(t, c, "commands.summon.success")
	killer := equineAttributes(t, c, "@e[tag=k,limit=1]")
	if len(killer) != 2 || killer["minecraft:attack_damage"]["base"] != nbt.Double(3) || killer["minecraft:armor"]["base"] != nbt.Double(8) {
		t.Errorf("killer bunny attributes = %v, want attack_damage 3 and armor 8", killer)
	}
	if _, ok := killer["minecraft:attack_damage"]["modifiers"]; ok {
		t.Errorf("killer bunny saved its transient modifier: %v", killer)
	}
	tags := map[string]string{"r": "rabbit", "p": "pig", "k": "rabbit"}
	// Ticked bodies save movement_speed too, which the reload keeps.
	servertest.RunTicks(t, srv, 1)
	c.Settle()
	want := map[string]map[string]nbt.Compound{}
	for tag := range tags {
		want[tag] = equineAttributes(t, c, "@e[tag="+tag+",limit=1]")
		if want[tag]["minecraft:movement_speed"] == nil {
			t.Errorf("%s ticked attributes = %v, want movement_speed", tag, want[tag])
		}
	}
	srv.FlushMobs()
	c.Disconnect()
	srv.Shutdown()
	again, connect := servertest.Start(t, cfg, servertest.OpsListing(t, "Rider"))
	servertest.FreezeUntilHeld(t, again)
	c = connect().Join("Rider")
	servertest.HoldTicks(t, again)
	servertest.UntilTicks(t, again, 20, "saved mobs wake", func() bool { return again.MobCount() == len(tags) })
	c.Settle()
	checkMobAttributeFixtures(t, c, "reloaded")
	for tag := range tags {
		got := equineAttributes(t, c, "@e[tag="+tag+",limit=1]")
		// A reloaded body has not ticked, so only the loaded entries remain.
		if !reflect.DeepEqual(got, want[tag]) {
			t.Errorf("%s reloaded attributes = %v, want %v", tag, got, want[tag])
		}
	}
	// The transient variant modifier is rebuilt from RabbitType on load.
	if got := attributeValue(t, c, "@e[tag=k,limit=1]", "attack_damage"); got != "8.0" {
		t.Errorf("reloaded killer bunny attack = %s, want 8.0", got)
	}
}

func checkMobAttributeFixtures(t *testing.T, c *servertest.Client, stage string) {
	t.Helper()
	data, err := os.ReadFile("testdata/mob_attribute_instances.tsv")
	if err != nil {
		t.Fatal(err)
	}
	wants := map[string]map[string]nbt.Compound{}
	for line := range strings.SplitSeq(strings.TrimSpace(string(data)), "\n") {
		if strings.HasPrefix(line, "#") {
			continue
		}
		fields := strings.Split(line, "\t")
		if len(fields) != 4 {
			t.Fatalf("invalid attribute fixture: %q", line)
		}
		if fields[0] != stage {
			continue
		}
		tag, id, want := fields[1], fields[2], fields[3]
		entry, err := nbt.ParseSNBT(want)
		if err != nil {
			t.Fatal(err)
		}
		if wants[tag] == nil {
			wants[tag] = map[string]nbt.Compound{}
		}
		wants[tag][id] = entry
		selector := "@e[tag=" + tag + ",limit=1]"
		path := `attributes[{id:"` + id + `"}]`
		if got := livingData(t, c, selector, path); got != want {
			t.Errorf("%s %s /data: %s, want %s", stage, tag, got, want)
		}
		for _, plain := range []string{"", ",plain:1b"} {
			c.ChatCommand(`tellraw @s {nbt:'` + path + `',entity:'` + selector + `'` + plain + `}`)
			line := servertest.FeedbackComponent(t, c)
			if got := line.Plain(); got != want {
				t.Errorf("%s %s component %q: %s, want %s", stage, tag, plain, got, want)
			}
		}
	}
	if len(wants) != 2 {
		t.Fatalf("%s fixtures cover %d mobs, want 2", stage, len(wants))
	}
	// AttributeMap's holder identities can change list order between JVMs.
	for tag, want := range wants {
		if got := equineAttributes(t, c, "@e[tag="+tag+",limit=1]"); !reflect.DeepEqual(got, want) {
			t.Errorf("%s %s attributes = %v, want %v", stage, tag, got, want)
		}
	}
}

// attributeValue is the value `/attribute <selector> <attribute> get` reports.
func attributeValue(t *testing.T, c *servertest.Client, selector, attribute string) string {
	t.Helper()
	c.ChatCommand("attribute " + selector + " " + attribute + " get")
	var got string
	c.SettleAndCollect(func(id int32, data []byte) {
		if id != protocol.IDPlaySystemChat {
			return
		}
		text, ok := protocol.ReadText(protocol.NewReader(data))
		if ok && text.Translate == "commands.attribute.value.get.success" {
			got = text.With[len(text.With)-1].Text
		}
	})
	return got
}
