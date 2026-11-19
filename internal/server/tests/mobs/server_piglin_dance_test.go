package mobs

import (
	"fmt"
	"testing"

	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/internal/server/servertest"
)

func TestPiglinDanceRollIsSharedByGameTick(t *testing.T) {
	t.Parallel()
	// Golden outcomes from SingleThreadedRandomSource(gameTime).nextFloat().
	for _, tc := range []struct {
		time  int64
		dance bool
	}{
		{1, false}, {4096, true}, {5000, true}, {12345, false},
	} {
		t.Run(fmt.Sprint(tc.time), func(t *testing.T) {
			c, _, srv := barterWorld(t)
			hoglin := srv.SpawnMob("minecraft:hoglin", 5.5, 64, .5)
			srv.SetMobBaby(hoglin, false)
			srv.SetMobNoAI(hoglin)
			piglins := map[int32]int64{}
			for i := range 4 {
				p := srv.SpawnMob(barterPiglin, 3.5+float64(i), 64, 3.5)
				srv.SetMobBaby(p, false)
				seed := int64(1)
				if i == 0 {
					seed = 9
				}
				piglins[p] = seed
				srv.HurtMobByMob(p, hoglin, .1, "minecraft:arrow")
			}
			servertest.RunTicks(t, srv, 2)
			c.Settle()
			for p, seed := range piglins {
				if srv.MobTargetID(p) != hoglin {
					t.Fatalf("piglin %d is not targeting the hoglin", p)
				}
				// Seed 9 rolls low in the mob stream, seed 1 high.
				srv.SeedMob(p, seed)
			}
			srv.SetWorldAge(tc.time - 1)
			if !srv.KillMob(hoglin) {
				t.Fatal("hoglin already dead")
			}
			dancing := map[int32]bool{}
			for range 2 {
				servertest.RunTicks(t, srv, 1)
				c.SettleAndCollect(func(id int32, data []byte) {
					if id != protocol.IDPlayEntityMetadata {
						return
					}
					r := protocol.NewReader(data)
					p := r.ReadVarInt()
					if piglins[p] == 0 {
						return
					}
					entries, ok := servertest.ReadMetadata(r)
					if !ok {
						t.Fatal("invalid piglin metadata")
					}
					if e := servertest.MetaOf(entries, protocol.MetadataIndexPiglinDancing); e != nil {
						dancing[p] = e.FlagVal
					}
				})
			}
			for p := range piglins {
				if dancing[p] != tc.dance {
					t.Errorf("piglin %d dancing = %v in game tick %d, want %v", p, dancing[p], tc.time, tc.dance)
				}
				if srv.MobTargetID(p) != 0 {
					t.Errorf("piglin %d still targets the dead hoglin", p)
				}
			}
		})
	}
}
