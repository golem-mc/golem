package entities

import (
	"fmt"
	"slices"
	"testing"

	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/internal/server/servertest"
)

// vanilla: AbstractArrow.onHitEntity, LivingEntity.tick
func TestMannequinLodgedArrowRemoval(t *testing.T) {
	for _, kind := range []string{"arrow", "spectral_arrow"} {
		t.Run(kind, func(t *testing.T) {
			t.Parallel()
			srv, c := joinMannequinOp(t, "ArrowWatcher")
			c.ChatCommand("summon mannequin 3.5 64 3.5 {immovable:1b}")
			id := awaitMannequin(t, c, srv).id
			c.ChatCommand("summon " + kind + " 3.5 65 2 {Motion:[0d,0d,1.5d],NoGravity:1b}")
			c.Settle()
			if got := servertest.StuckArrows(t, c, srv, id, 2); !slices.Equal(got, []int32{1}) {
				t.Fatalf("arrow counts after hit = %v, want [1]", got)
			}
			c.ChatCommand("data merge entity @e[type=mannequin,limit=1] {Silent:1b}")
			if got := servertest.StuckArrows(t, c, srv, id, 0); !slices.Equal(got, []int32{1}) {
				t.Fatalf("arrow count after /data = %v, want [1]", got)
			}
			// Mannequins step before arrows, so the countdown starts after the hit tick.
			if got := servertest.StuckArrows(t, c, srv, id, 578); len(got) != 0 {
				t.Fatalf("arrow counts before 580 ticks = %v, want none", got)
			}
			if got := servertest.StuckArrows(t, c, srv, id, 1); !slices.Equal(got, []int32{0}) {
				t.Fatalf("arrow counts at 580 ticks = %v, want [0]", got)
			}
		})
	}
}

// vanilla: AbstractArrow.onHitEntity, EnchantmentHelper.doPostAttackEffectsWithItemSource
func TestMannequinProjectileThorns(t *testing.T) {
	for _, kind := range []string{"arrow", "spectral_arrow", "trident"} {
		for _, refused := range []bool{false, true} {
			t.Run(fmt.Sprintf("%s/refused=%v", kind, refused), func(t *testing.T) {
				t.Parallel()
				srv, c := joinMannequinOp(t, "ThornsArcher")
				c.ChatCommand("gamemode survival")
				c.MoveToGround(0.5, 64, 3.5, true)
				tag := ""
				if refused {
					tag = ",Invulnerable:1b"
				}
				c.ChatCommand(`summon mannequin 3.5 64 3.5 {immovable:1b,equipment:{offhand:{id:"minecraft:diamond_chestplate",count:1,components:{"minecraft:enchantments":{"minecraft:thorns":7}}}}` + tag + `}`)
				awaitMannequin(t, c, srv)
				owner := servertest.UuidWords(protocol.OfflineUUID("ThornsArcher"))
				c.ChatCommand(fmt.Sprintf("summon %s 3.5 65 2 {Motion:[0d,0d,1.5d],NoGravity:1b,Owner:[I;%d,%d,%d,%d]}", kind, owner[0], owner[1], owner[2], owner[3]))
				c.Settle()
				shooterID := c.PlayerEntityID
				var damaged, pushed bool
				c.ReadTicks(srv, 2, func(id int32, data []byte) bool {
					r := protocol.NewReader(data)
					if id == protocol.IDPlayDamageEvent && r.ReadVarInt() == shooterID {
						damaged = true
					}
					if id == protocol.IDPlaySetEntityMotion && r.ReadVarInt() == shooterID {
						pushed = true
					}
					return false
				})
				health := srv.PlayerHealth("ThornsArcher")
				if refused {
					if health != 20 || damaged || pushed {
						t.Fatalf("refused hit: shooter health=%v damaged=%v pushed=%v", health, damaged, pushed)
					}
				} else {
					if health < 15 || health > 19 || !damaged || !pushed {
						t.Fatalf("landed hit: shooter health=%v damaged=%v pushed=%v, want 15..19 and damage/motion", health, damaged, pushed)
					}
					if got := dataOfMannequin(t, c, srv, `equipment.offhand.components."minecraft:damage"`); got != "2" {
						t.Errorf("Thorns equipment wear = %s, want 2", got)
					}
				}
			})
		}
	}
}
