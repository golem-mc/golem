package mobs

import (
	"testing"

	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/internal/server/servertest"
)

func TestAPiglinCountsOnlyAdultsWhenHurtByAHoglin(t *testing.T) {
	for _, tc := range []struct {
		name, ally string
		retreat    bool
	}{
		{"babies", "", true},
		{"adult", barterPiglin, false},
		{"brute", bruteKind, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			c, _, srv := piglinWorld(t)
			c.ChatCommand("gamemode creative Slayer")
			c.ChatCommand(`summon minecraft:piglin 8.5 64 6.5 {Tags:["subject"],IsBaby:0b,IsImmuneToZombification:1b,CannotHunt:1b}`)
			c.Settle()
			var subject int32
			for _, m := range srv.Mobs() {
				if m.Kind == barterPiglin {
					subject = m.ID
				}
			}
			if subject == 0 {
				t.Fatal("no piglin summoned")
			}
			for _, x := range []float64{5.5, 11.5} {
				baby := srv.SpawnMob(barterPiglin, x, 64, 6.5)
				srv.SetMobBaby(baby, true)
				srv.SetMobNoAI(baby)
			}
			if tc.ally != "" {
				ally := srv.SpawnMob(tc.ally, 8.5, 64, 3.5)
				srv.SetMobBaby(ally, false)
				srv.SetMobNoAI(ally)
			}
			var attacker int32
			for _, x := range []float64{8.5, 12.5} {
				attacker = srv.SpawnMob(hoglin, x, 64, 11.5)
				srv.SetMobBaby(attacker, false)
				srv.SetMobNoAI(attacker)
			}
			uuid, ok := srv.MobUUID(attacker)
			if !ok {
				t.Fatal("no attacking hoglin")
			}
			c.ChatCommand("damage @e[tag=subject,limit=1] 1 minecraft:mob_attack by " + uuid)
			c.Settle()
			retreated := c.ReadTicks(srv, 8, func(id int32, data []byte) bool {
				return id == protocol.IDPlaySoundEffect && servertest.SoundIs(data, "minecraft:entity.piglin.retreat")
			})
			if retreated != tc.retreat {
				t.Errorf("retreat sound = %v, want %v", retreated, tc.retreat)
			}
			wantTarget := attacker
			if tc.retreat {
				wantTarget = 0
			}
			if got := srv.MobTargetID(subject); got != wantTarget {
				t.Errorf("target = %d, want %d", got, wantTarget)
			}
		})
	}
}
