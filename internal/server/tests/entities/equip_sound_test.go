package entities

import (
	"testing"

	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/internal/server"
	"github.com/golem-mc/golem/internal/server/servertest"
)

// equipSounds reads n ticks and returns the category of each Sound Effect
// named want, in order.
func equipSounds(t *testing.T, c *servertest.Client, srv *server.Server, n int, want string) []int32 {
	t.Helper()
	id := soundID(t, want)
	var cats []int32
	c.ReadTicks(srv, n, func(pid int32, data []byte) bool {
		if pid == protocol.IDPlaySoundEffect {
			if s := servertest.DecodePlaySoundEffect(t, data); s.RegistryID == id {
				cats = append(cats, s.Category)
			}
		}
		return false
	})
	return cats
}

// vanilla: ArmorStand.swapItem, EntityItemAccessor, LivingEntity.onEquipItem
func TestEquipSoundOnStandUseAndItemReplace(t *testing.T) {
	t.Parallel()
	srv, c := servertest.Join(t, servertest.MobTestConfig(), "Op", servertest.OpsListing(t, "Op"))
	servertest.HoldTicks(t, srv)
	c.ChatCommand("give @s diamond_helmet")
	servertest.AwaitSystemChatSaying(t, c, "commands.give.success.single")
	c.ChatCommand("summon minecraft:armor_stand 0.5 64 2.5")
	stand := c.AwaitSpawn(protocol.EntityTypeArmorStand)
	servertest.AwaitSystemChatSaying(t, c, "commands.summon.success")
	c.Settle()

	c.UseOn(stand.EntityID)
	if got := equipSounds(t, c, srv, 3, "minecraft:item.armor.equip_diamond"); len(got) != 1 || got[0] != protocol.SoundCategoryNeutral {
		t.Errorf("stand use equip sounds = %v, want one neutral", got)
	}

	for _, tc := range []struct {
		tag  string
		want int
	}{{"", 1}, {",Silent:1b", 0}} {
		c.ChatCommand("summon minecraft:zombie 0.5 64 4.5 {NoAI:1b" + tc.tag + "}")
		c.AwaitSpawn(protocol.EntityTypeZombie)
		servertest.AwaitSystemChatSaying(t, c, "commands.summon.success")
		c.ChatCommand("item replace entity @e[type=minecraft:zombie,limit=1] armor.head with minecraft:golden_helmet")
		got := equipSounds(t, c, srv, 3, "minecraft:item.armor.equip_gold")
		if len(got) != tc.want || (tc.want == 1 && got[0] != protocol.SoundCategoryHostile) {
			t.Errorf("zombie %q: equip_gold categories = %v, want %d hostile", tc.tag, got, tc.want)
		}
		c.ChatCommand("kill @e[type=minecraft:zombie]")
		servertest.AwaitSystemChatSaying(t, c, "commands.kill.success")
	}
}
