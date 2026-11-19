package commands

import (
	"fmt"
	"testing"

	"github.com/golem-mc/golem/internal/server/servertest"
	"github.com/google/uuid"
)

// vanilla: Villager.getTypeName, Entity.getName, SummonCommand.spawnEntity
func TestVillagerFeedbackNamesItsProfession(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		profession, plain, custom, kind string
	}{
		{"farmer", "Farmer", "", "villager"},
		{"armorer", "Armorer", "", "villager"},
		{"none", "Villager", "", "villager"},
		{"nitwit", "Nitwit", "", "villager"},
		{"farmer", "Bob", `,CustomName:"Bob"`, "villager"},
		{"farmer", "Zombie Villager", "", "zombie_villager"},
	} {
		t.Run(tc.kind+"/"+tc.plain, func(t *testing.T) {
			t.Parallel()
			_, c := servertest.Join(t, servertest.MobTestConfig(), "Summoner", servertest.OpsListing(t, "Summoner"))
			identity := uuid.MustParse("00000001-0000-0002-0000-000300000004")
			kind := "minecraft:" + tc.kind
			key := "entity.minecraft." + tc.kind
			if tc.kind == "villager" {
				key += "." + tc.profession
			}
			display := servertest.EntityDisplayComponent(kind, key, identity)
			if tc.custom != "" {
				display = namedDisplay(kind, tc.plain, 4)
			}

			c.ChatCommand(fmt.Sprintf(`summon %s 3.5 64 3.5 {UUID:[I;1,2,3,4],NoAI:1b,VillagerData:{profession:"minecraft:%s"}%s}`,
				kind, tc.profession, tc.custom))
			servertest.AwaitCommandFeedbackText(t, c, "commands.summon.success", display)

			// The selector's plain name must agree with the feedback's component.
			c.ChatCommand(fmt.Sprintf(`kill @e[type=%s,name="%s",limit=1]`, kind, tc.plain))
			servertest.AwaitCommandFeedbackText(t, c, "commands.kill.success.single", display)
		})
	}
}
