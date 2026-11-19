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
