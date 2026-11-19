package commands

import (
	"fmt"
	"testing"

	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/internal/server/servertest"
	"github.com/golem-mc/golem/registry"
)

// vanilla: LivingEntity.readAdditionalSaveData, setHealth, tickDeath (without die).
func TestMobNonPositiveNBTHealthDiesWithoutKillEffects(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name, kind, health string
		summon             bool
		deathTime          int
	}{
		{"merge-zero", "zombie", "0f", false, 0},
		{"merge-negative", "zombie", "-5f", false, 0},
		{"summon-zero", "zombie", "0f", true, 0},
		{"summon-negative", "zombie", "-5f", true, 0},
		{"saved-death-time", "zombie", "0f", true, 7},
		{"cow", "cow", "0f", false, 0},
		{"pet", "wolf", "0f", false, 0},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			srv, c := servertest.Join(t, servertest.MobTestConfig(), "Tester", servertest.OpsListing(t, "Tester"))
			servertest.HoldTicks(t, srv)
			srv.SetDayTime(18000)
			tag := fmt.Sprintf(`{NoAI:1b,Owner:"%s",equipment:{mainhand:{id:"minecraft:diamond"}},drop_chances:{mainhand:1f}`,
				protocol.OfflineUUID("Tester"))
			if tc.summon {
				tag += fmt.Sprintf(",Health:%s,DeathTime:%ds", tc.health, tc.deathTime)
			}
			c.ChatCommand("summon " + tc.kind + " 4.5 64 4.5 " + tag + "}")
			kind, _ := registry.EntityTypeByName("minecraft:" + tc.kind)
			mob := c.ExpectMobSpawn(int32(kind))
			servertest.AwaitSystemChatSaying(t, c, "commands.summon.success")
			if !tc.summon {
				c.ChatCommand("data merge entity " + mob.Uuid.String() + " {Health:" + tc.health + "}")
				servertest.AwaitSystemChatSaying(t, c, "commands.data.entity.modified")
			}
			c.ChatCommand("execute if entity @e[type=" + tc.kind + "]")
			c.ExpectCommandFailure(servertest.CommandErrorMessageForTest("commands.execute.conditional.fail"))

			poof, removed := 0, 0
			zeroHealth := false
			for tick := 1; tick <= 30; tick++ {
				servertest.RunTicks(t, srv, 1)
				c.SettleAndCollect(func(id int32, data []byte) {
					r := protocol.NewReader(data)
					switch id {
					case protocol.IDPlayEntityMetadata:
						if r.ReadVarInt() != mob.EntityID {
							return
						}
						entries, ok := servertest.ReadMetadata(r)
						if !ok || r.Err() != nil {
							t.Fatal("invalid mob metadata")
						}
						if health := servertest.MetaOf(entries, protocol.MetadataIndexLivingHealth); health != nil {
							zeroHealth = health.FloatVal == 0
							if !zeroHealth {
								t.Errorf("dying body's health = %v, want 0", health.FloatVal)
							}
						}
					case protocol.IDPlayEntityEvent:
						if r.ReadI32() != mob.EntityID {
							return
						}
						switch r.ReadU8() {
						case protocol.EntityStatusDeath:
							t.Error("NBT health invoked the kill death event")
						case protocol.EntityStatusDeathPoof:
							poof = tick
						}
					case protocol.IDPlayRemoveEntities:
						for range r.ReadVarInt() {
							if r.ReadVarInt() == mob.EntityID {
								if poof == 0 {
									t.Error("body removed before its death poof")
								}
								removed = tick
							}
						}
					case protocol.IDPlaySpawnEntity:
						r.ReadVarInt()
						r.ReadUUID()
						if kind := r.ReadVarInt(); kind == protocol.EntityTypeItem || kind == protocol.EntityTypeExperienceOrb {
							t.Errorf("NBT health spawned loot or experience on tick %d", tick)
						}
					case protocol.IDPlaySystemChat:
						t.Error("NBT health sent a death message")
					}
				})
				if tick == 1 && !zeroHealth {
					t.Error("first death tick did not send zero health for the death animation")
				}
			}
			if want := 20 - tc.deathTime; poof != want || removed != want {
				t.Errorf("poof/removal ticks = %d/%d, want %d/%d", poof, removed, want, want)
			}
			c.ChatCommand("execute if entity @e[type=item]")
			c.ExpectCommandFailure(servertest.CommandErrorMessageForTest("commands.execute.conditional.fail"))
		})
	}
}
