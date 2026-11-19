package mobs

import (
	"reflect"
	"testing"

	"github.com/golem-mc/golem/internal/config"
	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/internal/server"
	"github.com/golem-mc/golem/internal/server/rules"
	"github.com/golem-mc/golem/internal/server/servertest"
	"github.com/golem-mc/golem/nbt"
	"github.com/golem-mc/golem/registry"
)

const infectOffers = `Offers:{Recipes:[{buy:{id:"minecraft:emerald",count:10},sell:{id:"minecraft:paper",count:1},maxUses:16,priceMultiplier:0.2f}]}`

// infectionYard stands a zombie 6 blocks from a librarian villager with the
// only player in creative.
func infectionYard(t *testing.T, difficulty uint8) (*server.Server, *servertest.Client, int32, int32) {
	t.Helper()
	const name = "Bystander"
	cfg := servertest.HostileTestConfig()
	cfg.ViewDistance, cfg.SimulationDistance = config.MinDistance, config.MinDistance
	srv, c, _ := servertest.JoinAs(t, cfg, name, servertest.OpsListing(t, name))
	servertest.HoldTicks(t, srv)
	srv.SetDayTime(18000)
	srv.SetDifficulty(difficulty)
	srv.SetPlayerGameMode(name, protocol.GameModeCreative)
	c.MoveToGround(0.5, 64, 10.5, true)
	c.ChatCommand(`summon minecraft:villager 0.5 64 0.5 {NoAI:1b,Health:1f,VillagerData:{profession:"minecraft:librarian",level:3,type:"minecraft:desert"},Xp:42,Gossips:[{Target:[I;0,0,0,1],Type:"minor_positive",Value:25}],` + infectOffers + `}`)
	villager := c.AwaitSpawn(villagerType(t)).EntityID
	c.ChatCommand(`summon minecraft:zombie 6.5 64 0.5 {}`)
	zombieType, _ := registry.EntityTypeID("minecraft:zombie")
	zombie := c.AwaitSpawn(zombieType).EntityID
	c.Settle()
	return srv, c, villager, zombie
}

func TestAZombieInfectsTheVillagerItKillsOnHard(t *testing.T) {
	t.Parallel()
	for _, silentKiller := range []bool{false, true} {
		name := "silent_victim"
		if silentKiller {
			name = "silent_killer"
		}
		t.Run(name, func(t *testing.T) {
			srv, c, villager, zombie := infectionYard(t, rules.DifficultyHard)
			srv.SetMobSilent(zombie, silentKiller)
			srv.SetMobSilent(villager, !silentKiller)
			before := entityCompound(t, c, "@e[type=minecraft:villager,limit=1]")
			zombieVillager, _ := registry.EntityTypeID("minecraft:zombie_villager")
			var replacement int32
			var position [3]float64
			var eventPosition [3]int32
			var metadata [3]int32
			var event, removed, finalized bool
			servertest.UntilTicks(t, srv, 400, "the villager became a zombie villager", func() bool {
				c.SettleAndCollect(func(pid int32, data []byte) {
					r := protocol.NewReader(data)
					switch pid {
					case protocol.IDPlaySpawnEntity:
						id := r.ReadVarInt()
						r.ReadUUID()
						if r.ReadVarInt() == zombieVillager {
							replacement = id
							position = [3]float64{r.ReadF64(), r.ReadF64(), r.ReadF64()}
						}
					case protocol.IDPlayRemoveEntities:
						for range r.ReadVarInt() {
							removed = r.ReadVarInt() == villager || removed
						}
					case protocol.IDPlayWorldEvent:
						if r.ReadI32() == protocol.WorldEventZombieVillagerInfected {
							event = true
							x, y, z := r.ReadPosition()
							eventPosition = [3]int32{x, y, z}
						}
					case protocol.IDPlayEntityMetadata:
						if r.ReadVarInt() != replacement || replacement == 0 {
							return
						}
						for r.Err() == nil {
							index := r.ReadU8()
							if index == protocol.MetadataEnd {
								break
							}
							kind := r.ReadVarInt()
							switch {
							case index == protocol.MetadataIndexVillagerData && kind == protocol.MetadataVillagerData:
								metadata = [3]int32{r.ReadVarInt(), r.ReadVarInt(), r.ReadVarInt()}
							case index == protocol.MetadataIndexVillagerDataFinalized && kind == protocol.MetadataBoolean:
								finalized = r.ReadBool()
							default:
								servertest.SkipBodyEntry(c, r, kind)
							}
						}
					}
				})
				return replacement != 0
			})
			if !removed {
				t.Error("original villager was not removed from the client")
			}
			if _, ok := srv.MobHealth(villager); ok {
				t.Error("original villager remains on the server")
			}
			if position != [3]float64{0.5, 64, 0.5} {
				t.Errorf("replacement position = %v", position)
			}
			if metadata != [3]int32{0, professionLibrarian, 3} || !finalized {
				t.Errorf("villager metadata = %v, finalized = %v", metadata, finalized)
			}
			if event != !silentKiller {
				t.Errorf("infection event = %v, killer silent = %v", event, silentKiller)
			}
			if event {
				x, y, z, _ := srv.MobPos(zombie)
				want := [3]int32{rules.BlockOf(x), rules.BlockOf(y), rules.BlockOf(z)}
				if eventPosition != want {
					t.Errorf("event at %v, killer at %v", eventPosition, want)
				}
			}
			after := entityCompound(t, c, "@e[type=minecraft:zombie_villager,limit=1]")
			for _, key := range []string{"Offers", "VillagerData", "Xp", "Gossips"} {
				if before[key] == nil || !reflect.DeepEqual(before[key], after[key]) {
					t.Errorf("%s changed: before %v, after %v", key, before[key], after[key])
				}
			}
		})
	}
}

func TestAZombieLeavesAVillagerDeadOnEasy(t *testing.T) {
	t.Parallel()
	srv, c, villager, _ := infectionYard(t, rules.DifficultyEasy)
	zombieVillager, _ := registry.EntityTypeID("minecraft:zombie_villager")
	servertest.UntilTicks(t, srv, 400, "the villager died", func() bool {
		health, ok := srv.MobHealth(villager)
		return !ok || health <= 0
	})
	c.ExpectNoneWithinTicks(srv, 20, func(pid int32, data []byte) bool {
		r := protocol.NewReader(data)
		if pid != protocol.IDPlaySpawnEntity {
			return false
		}
		r.ReadVarInt()
		r.ReadUUID()
		return r.ReadVarInt() == zombieVillager
	})
}

func TestAZombieHuntsItsNonPlayerTargets(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name, hunter, target string
		baby                 bool
		health               float64
	}{
		{"golem", "zombie", "iron_golem", false, 100},
		{"trader", "zombie", "wandering_trader", false, 20},
		{"baby_turtle", "zombie", "turtle", true, 30},
		{"axolotl", "drowned", "axolotl", false, 14},
	} {
		t.Run(tc.name, func(t *testing.T) {
			srv, c, _, zombie := infectionYard(t, rules.DifficultyEasy)
			c.ChatCommand(`kill @e[type=minecraft:villager]`)
			if tc.hunter == "drowned" {
				c.ChatCommand(`kill @e[type=minecraft:zombie]`)
				zombie = srv.SpawnMob("minecraft:drowned", 6.5, 64, 0.5)
			}
			c.Settle()
			target := srv.SpawnMob("minecraft:"+tc.target, 0.5, 64, 0.5)
			srv.SetMobNoAI(target)
			if tc.baby {
				srv.SetMobBaby(target, true)
			}
			servertest.UntilTicks(t, srv, 400, "the hunter hurt its target", func() bool {
				health, _ := srv.MobHealth(target)
				return health < tc.health
			})
			if srv.MobTargetID(zombie) != target {
				t.Error("hunter did not select the expected target")
			}
		})
	}
}

func TestAZombieIgnoresAnAdultTurtle(t *testing.T) {
	t.Parallel()
	srv, c, _, zombie := infectionYard(t, rules.DifficultyEasy)
	c.ChatCommand(`kill @e[type=minecraft:villager]`)
	c.Settle()
	turtle := srv.SpawnMob("minecraft:turtle", 0.5, 64, 0.5)
	srv.SetMobNoAI(turtle)
	for range 100 {
		servertest.RunTicks(t, srv, 1)
		if srv.MobTargetID(zombie) == turtle {
			t.Fatal("zombie targeted an adult turtle")
		}
	}
	if health, _ := srv.MobHealth(turtle); health != 30 {
		t.Errorf("adult turtle health = %v, want 30", health)
	}
}

// entityData returns the compound printed by /data get entity.
func entityData(t *testing.T, c *servertest.Client, selector string) string {
	t.Helper()
	c.ChatCommand("data get entity " + selector)
	var result string
	c.SettleAndCollect(func(packetID int32, data []byte) {
		if packetID != protocol.IDPlaySystemChat {
			return
		}
		text, ok := protocol.ReadText(protocol.NewReader(data))
		if ok && text.Translate == "commands.data.entity.query" && len(text.With) == 2 {
			result = text.With[1].Plain()
		}
	})
	if result == "" {
		t.Fatalf("no /data get entity feedback for %s arrived", selector)
	}
	return result
}

func entityCompound(t *testing.T, c *servertest.Client, selector string) nbt.Compound {
	t.Helper()
	tag, err := nbt.ParseSNBT(entityData(t, c, selector))
	if err != nil {
		t.Fatal(err)
	}
	return tag
}
