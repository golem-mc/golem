package entities

import (
	"reflect"
	"testing"

	"github.com/golem-mc/golem/internal/server/servertest"
	"github.com/golem-mc/golem/nbt"
)

func equineAttributes(t *testing.T, c *servertest.Client, selector string) map[string]nbt.Compound {
	t.Helper()
	tag, err := nbt.ParseSNBT("{attributes:" + livingData(t, c, selector, "attributes") + "}")
	if err != nil {
		t.Fatal(err)
	}
	list, _ := nbt.Get[nbt.List](tag, "attributes")
	entries := make(map[string]nbt.Compound)
	for _, item := range list.Items {
		entry, _ := nbt.CompoundView(item)
		id, _ := nbt.Get[nbt.String](entry, "id")
		if _, exists := entries[string(id)]; exists {
			t.Errorf("duplicate attribute %s", id)
		}
		entries[string(id)] = entry
	}
	return entries
}

// vanilla: AttributeMap.pack, AttributeCommand.getAttributeBase, setAttributeBase
func TestEquineSavedAttributeMembership(t *testing.T) {
	t.Parallel()
	cfg := livingSavedConfig()
	srv, c := servertest.Join(t, cfg, "Rider", servertest.OpsListing(t, "Rider"))
	servertest.HoldTicks(t, srv)
	kinds := []string{"horse", "donkey", "mule", "skeleton_horse", "zombie_horse", "llama", "trader_llama", "camel", "camel_husk"}
	for _, kind := range kinds {
		t.Run(kind, func(t *testing.T) {
			c.ChatCommand("summon " + kind + " ~ ~ ~3 {NoAI:1b,PersistenceRequired:1b}")
			servertest.AwaitSystemChatSaying(t, c, "commands.summon.success")
			selector := "@e[type=" + kind + ",limit=1]"
			for _, attr := range []string{"movement_speed", "max_health", "jump_strength"} {
				c.ChatCommand("attribute " + selector + " " + attr + " base get")
				servertest.AwaitSystemChatSaying(t, c, "commands.attribute.base_value.get.success")
			}
			entries := equineAttributes(t, c, selector)
			if len(entries) != 1 || entries["minecraft:movement_speed"] == nil {
				t.Errorf("queried defaults = %v, want only movement_speed", entries)
			}
		})
	}
	selector := "@e[type=zombie_horse,limit=1]"
	for _, edit := range []string{
		"max_health base set 25", // Instantiation persists even when the base is unchanged.
		"jump_strength base set 0.7",
		"movement_speed base set 0.3",
		"jump_strength modifier add test:jump 0.2 add_value",
	} {
		c.ChatCommand("attribute " + selector + " " + edit)
		c.Settle()
	}
	want := equineAttributes(t, c, selector)
	if len(want) != 3 || want["minecraft:max_health"]["base"] != nbt.Double(25) ||
		want["minecraft:movement_speed"]["base"] != nbt.Double(0.3) || want["minecraft:jump_strength"]["base"] != nbt.Double(0.7) {
		t.Fatalf("edited attributes = %v", want)
	}
	mods, _ := nbt.Get[nbt.List](want["minecraft:jump_strength"], "modifiers")
	if len(mods.Items) != 1 {
		t.Fatalf("jump modifiers = %v, want test:jump", mods)
	}
	modifier, _ := nbt.CompoundView(mods.Items[0])
	if modifier["id"] != nbt.String("test:jump") || modifier["amount"] != nbt.Double(0.2) || modifier["operation"] != nbt.String("add_value") {
		t.Fatalf("jump modifier = %v", modifier)
	}
	for _, attr := range []string{"max_health", "movement_speed", "jump_strength"} {
		c.ChatCommand("attribute @e[type=horse,limit=1] " + attr + " base set 0")
		servertest.AwaitSystemChatSaying(t, c, "commands.attribute.base_value.set.success")
	}
	zero := equineAttributes(t, c, "@e[type=horse,limit=1]")
	if len(zero) != 3 {
		t.Fatalf("zero-valued edits = %v, want all three attributes", zero)
	}
	for id, entry := range zero {
		if entry["base"] != nbt.Double(0) {
			t.Errorf("%s base = %v, want 0", id, entry["base"])
		}
	}
	srv.FlushMobs()
	c.Disconnect()
	srv.Shutdown()
	again, connect := servertest.Start(t, cfg, servertest.OpsListing(t, "Rider"))
	servertest.FreezeUntilHeld(t, again)
	c = connect().Join("Rider")
	servertest.HoldTicks(t, again)
	servertest.UntilTicks(t, again, 20, "saved equines wake", func() bool { return again.MobCount() == len(kinds) })
	c.Settle()
	if got := equineAttributes(t, c, selector); !reflect.DeepEqual(got, want) {
		t.Errorf("reloaded attributes = %v, want %v", got, want)
	}
	if got := equineAttributes(t, c, "@e[type=horse,limit=1]"); !reflect.DeepEqual(got, zero) {
		t.Errorf("reloaded zero-valued edits = %v, want %v", got, zero)
	}
	for _, kind := range kinds {
		if kind == "zombie_horse" || kind == "horse" {
			continue
		}
		if got := equineAttributes(t, c, "@e[type="+kind+",limit=1]"); len(got) != 1 || got["minecraft:movement_speed"] == nil {
			t.Errorf("%s reloaded defaults = %v, want only movement_speed", kind, got)
		}
	}
}

// vanilla: AbstractHorse.finalizeSpawn, Horse, AbstractChestedHorse,
// SkeletonHorse, ZombieHorse.randomizeAttributes
func TestEquineSpawnAttributesPersist(t *testing.T) {
	t.Parallel()
	cfg := livingSavedConfig()
	srv, c := servertest.Join(t, cfg, "Rider", servertest.OpsListing(t, "Rider"))
	servertest.HoldTicks(t, srv)
	wants := make(map[string]map[string]nbt.Compound)
	for _, tc := range []struct {
		kind         string
		health, jump bool
	}{
		{"horse", true, true}, {"donkey", true, false}, {"mule", true, false},
		{"llama", true, false}, {"trader_llama", true, false},
		{"skeleton_horse", false, true}, {"zombie_horse", false, true},
		{"camel", false, false}, {"camel_husk", false, false},
	} {
		t.Run(tc.kind, func(t *testing.T) {
			selector := "@e[type=" + tc.kind + ",limit=1]"
			// Omitting NBT invokes finalizeSpawn, unlike the defaults regression.
			c.ChatCommand("summon " + tc.kind + " ~ ~ ~3")
			servertest.AwaitSystemChatSaying(t, c, "commands.summon.success")
			c.ChatCommand("data merge entity " + selector + " {NoAI:1b,PersistenceRequired:1b}")
			servertest.AwaitSystemChatSaying(t, c, "commands.data.entity.modified")
			entries := equineAttributes(t, c, selector)
			for attr, present := range map[string]bool{"movement_speed": true, "follow_range": true, "max_health": tc.health, "jump_strength": tc.jump} {
				if got := entries["minecraft:"+attr] != nil; got != present {
					t.Errorf("%s membership = %v, want %v: %v", attr, got, present, entries)
				}
			}
			wants[tc.kind] = entries
		})
	}
	srv.FlushMobs()
	c.Disconnect()
	srv.Shutdown()
	again, connect := servertest.Start(t, cfg, servertest.OpsListing(t, "Rider"))
	servertest.FreezeUntilHeld(t, again)
	c = connect().Join("Rider")
	servertest.HoldTicks(t, again)
	servertest.UntilTicks(t, again, 20, "saved equines wake", func() bool { return again.MobCount() == len(wants) })
	c.Settle()
	for kind, want := range wants {
		if got := equineAttributes(t, c, "@e[type="+kind+",limit=1]"); !reflect.DeepEqual(got, want) {
			t.Errorf("%s reloaded attributes = %v, want %v", kind, got, want)
		}
	}
}
