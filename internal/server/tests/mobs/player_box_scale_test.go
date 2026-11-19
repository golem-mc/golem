package mobs

import (
	"fmt"
	"strings"
	"testing"

	"github.com/golem-mc/golem/internal/server/servertest"
)

// TestSelectorBoxFollowsScale: a volume above a half-scale player's head
// does not match them, one over their body does.
// vanilla: EntitySelector, Entity.getBoundingBox
func TestSelectorBoxFollowsScale(t *testing.T) {
	t.Parallel()
	srv, c := servertest.Join(t, servertest.HostileTestConfig(), "Tiny", servertest.OpsListing(t, "Tiny"))
	servertest.HoldTicks(t, srv)
	c.ChatCommand("attribute @s minecraft:scale base set 0.5")
	servertest.AwaitSystemChatSaying(t, c, "commands.attribute.base_value.set.success")
	holds := func(dy string) bool {
		out := srv.RunLine("execute as @a at @s positioned ~ ~" + dy + " ~ if entity @e[type=minecraft:player,dx=0,dy=0,dz=0]")
		return strings.Contains(fmt.Sprint(out), "conditional.pass")
	}
	if holds("1.05") {
		t.Error("a volume above the head matched the half-scale player")
	}
	if !holds("0.5") {
		t.Error("a volume over the body did not match the half-scale player")
	}
}
