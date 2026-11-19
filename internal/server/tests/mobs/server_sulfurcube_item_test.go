package mobs

import (
	"fmt"
	"testing"

	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/internal/server/servertest"
	"github.com/golem-mc/golem/nbt"
)

// vanilla: SulfurCube.getEquipmentSlotForItem, LivingEntity.createEquipmentSlotAccess
func TestSulfurCubeItemBodyRestrictions(t *testing.T) {
	t.Parallel()
	cfg := servertest.HostileTestConfig()
	cfg.GameMode = "creative"
	srv, c := servertest.Join(t, cfg, "Keeper", servertest.OpsListing(t, "Keeper"))
	servertest.HoldTicks(t, srv)
	for i, tc := range []struct {
		name, item string
		age        int
		accepted   bool
	}{
		{"adult stone", "stone", 0, true},
		{"baby stone", "stone", -24000, true},
		{"adult stick", "stick", 0, false},
		{"baby stick", "stick", -24000, false},
		{"adult horse armor", "iron_horse_armor", 0, true},
		{"baby horse armor", "iron_horse_armor", -24000, false},
		{"adult custom body", `stick[equippable={slot:"body"}]`, 0, true},
		{"baby custom body", `stick[equippable={slot:"body"}]`, -24000, false},
		{"swallowable overrides component", `stone[equippable={slot:"saddle"}]`, 0, true},
		{"baby empty", "air", -24000, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			selector := fmt.Sprintf("@e[tag=cube%d,limit=1]", i)
			c.ChatCommand(fmt.Sprintf(`summon minecraft:sulfur_cube 4.5 64 7.5 {NoAI:1b,Age:%d,Tags:["cube%d"]}`, tc.age, i))
			c.ExpectFeedbackLine("commands.summon.success")
			c.ChatCommand("item replace entity " + selector + " armor.body with minecraft:" + tc.item)
			key := "commands.item.target.failed.known_item"
			if tc.accepted {
				key = "commands.item.entity.replace.success.single.known_item"
			}
			expectSulfurItemFeedback(t, c, key)
			tag := entityCompound(t, c, selector)
			equipment, _ := nbt.Get[nbt.Compound](tag, "equipment")
			body, hasBody := nbt.Get[nbt.Compound](equipment, "body")
			if want := tc.accepted && tc.item != "air"; hasBody != want {
				t.Fatalf("body = %v (present %v), want present %v", body, hasBody, want)
			}
		})
	}
}

func expectSulfurItemFeedback(t *testing.T, c *servertest.Client, key string) {
	t.Helper()
	tag, _, err := nbt.DecodeNetwork(c.ExpectPlay(protocol.IDPlaySystemChat))
	if err != nil {
		t.Fatal(err)
	}
	compound, ok := tag.(nbt.Compound)
	if !ok {
		t.Fatalf("item feedback = %v, want a compound", tag)
	}
	if extra, ok := nbt.Get[nbt.List](compound, "extra"); ok && len(extra.Items) == 1 {
		compound, _ = extra.Items[0].(nbt.Compound)
	}
	if got, _ := nbt.Get[nbt.String](compound, "translate"); string(got) != key {
		t.Fatalf("item feedback = %v, want %q", tag, key)
	}
}
