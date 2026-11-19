package combat

import (
	"fmt"
	"github.com/golem-mc/golem/internal/server/servertest"
	"testing"

	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/internal/server"
	"github.com/golem-mc/golem/internal/server/entity"
	"github.com/golem-mc/golem/internal/server/player"
	"github.com/golem-mc/golem/registry"
)

// thornsWearer gives a player a Thorns III chestplate in the off hand, where
// the armour's own wear never reaches it and only the thorns' cost shows, and
// a diamond suit on so a long fight leaves them standing.
func thornsWearer(t *testing.T, srv *server.Server, name string) {
	t.Helper()
	srv.GiveSlot(name, player.OffHandSlot, servertest.EnchantedStack(t, "minecraft:diamond_chestplate", "minecraft:thorns", 3))
	for slot, item := range map[int]string{
		player.ArmorSlot + 1: "minecraft:diamond_chestplate",
		player.ArmorSlot:     "minecraft:diamond_helmet",
		player.ArmorSlot + 2: "minecraft:diamond_leggings",
		player.ArmorSlot + 3: "minecraft:diamond_boots",
	} {
		id, ok := registry.ItemID(item)
		if !ok {
			t.Fatalf("the item registry has no %s", item)
		}
		srv.GiveSlot(name, slot, protocol.ItemStack{ItemID: id, Count: 1})
	}
}

// TestThornsHurtsAPlayerWhoPunchesItsWearer punches a Thorns III wearer
// until the thorns answer (a 45% chance per landed hit): the puncher loses
// one to five health and the chestplate two durability, no more.
func TestThornsHurtsAPlayerWhoPunchesItsWearer(t *testing.T) {
	t.Parallel()
	srv, connect := servertest.Start(t, servertest.SurvivalConfig())

	victim := connect()
	knightID := victim.JoinAs("Knight")

	attacker := connect()
	attacker.JoinAs("Brute")

	victim.ExpectPlayerAdded("Brute")
	victim.ExpectAnnouncement("multiplayer.player.joined", "Brute")

	attacker.MoveToGround(0.5, 64, 0.5, true)
	victim.MoveToGround(3.5, 64, 0.5, true)
	attacker.Settle()
	victim.Settle()
	thornsWearer(t, srv, "Knight")

	for hits := 0; hits < 60 && srv.PlayerHealth("Brute") >= 20; hits++ {
		servertest.AwaitFullSwing(t, srv, "Brute")
		attacker.Send(protocol.IDPlayAttackIn, func(b *protocol.Buffer) { b.PutVarInt(knightID) })
		// The window the hit opened on the wearer must run out before the next.
		since := srv.TicksRun()
		servertest.UntilTicks(t, srv, 200, "condition", func() bool { return srv.TicksRun() >= since+12 })
	}
	lost := 20 - srv.PlayerHealth("Brute")
	if lost < 1 || lost > 5 {
		t.Errorf("the puncher lost %v health to the thorns, want 1 to 5", lost)
	}
	if wear := srv.InventorySlot("Knight", player.OffHandSlot).Damage; wear != 2 {
		t.Errorf("the chestplate took %d wear for one thorns hit, want 2", wear)
	}
}

// TestThornsHurtsAMobThatHitsItsWearer lets a zombie hit a Thorns III wearer
// until the thorns answer; the zombie loses one to five health.
func TestThornsHurtsAMobThatHitsItsWearer(t *testing.T) {
	t.Parallel()
	srv, c := servertest.Join(t, servertest.HostileTestConfig(), "Knight")
	servertest.NightWorld(srv, c)
	thornsWearer(t, srv, "Knight")

	servertest.HoldTicks(t, srv)
	zombieID := srv.SpawnMob("minecraft:zombie", 4.5, 64, 4.5)
	var health float64
	servertest.UntilTicks(t, srv, servertest.HostileTicks, "condition", func() bool {
		health, _ = srv.MobHealth(zombieID)
		return health < 20
	})
	if lost := 20 - health; lost < 1 || lost > 5 {
		t.Errorf("the zombie lost %v health to the thorns, want 1 to 5", lost)
	}
	if wear := srv.InventorySlot("Knight", player.OffHandSlot).Damage; wear%2 != 0 || wear < 2 {
		t.Errorf("the chestplate took %d wear, want a multiple of the two a thorns hit costs", wear)
	}
}

// TestAMobKilledByThornsCreditsItsWearer leaves a zombie half a heart: the
// first thorns hit kills it, and the wearer is paid the zombie's five
// experience as for a kill of their own.
func TestAMobKilledByThornsCreditsItsWearer(t *testing.T) {
	t.Parallel()
	srv, c := servertest.Join(t, servertest.HostileTestConfig(), "Knight")
	servertest.NightWorld(srv, c)
	thornsWearer(t, srv, "Knight")

	servertest.HoldTicks(t, srv)
	zombieID := srv.SpawnMob("minecraft:zombie", 4.5, 64, 4.5)
	if !srv.SetMobHealth(zombieID, 0.5) {
		t.Fatal("the zombie is not in the herd")
	}
	w := servertest.NewOrbWatcher(t, c)
	worth := func() int {
		total := 0
		for _, v := range w.OrbIDs {
			total += v
		}
		return total
	}
	w.ReadUntilTicks(srv, 2400, func() bool { return worth() >= 5 })
	got := worth()
	if got != 5 {
		t.Errorf("the zombie killed by thorns left orbs worth %d, want the 5 of a player's kill", got)
	}
}

// TestThornsPlaysItsHitSoundAtTheMob waits for the sound the thorns hit
// plays at a zombie that struck its wearer.
func TestThornsPlaysItsHitSoundAtTheMob(t *testing.T) {
	t.Parallel()
	srv, c := servertest.Join(t, servertest.HostileTestConfig(), "Knight")
	servertest.NightWorld(srv, c)
	thornsWearer(t, srv, "Knight")

	srv.SpawnMob("minecraft:zombie", 4.5, 64, 4.5)
	found := false
	c.PumpTicks(srv, 2400, func(id int32, data []byte) bool {
		if id != protocol.IDPlaySoundEffect {
			return false
		}
		sound, category, _, _, _, _, _ := servertest.ReadAmbient(t, data)
		if sound == "minecraft:enchant.thorns.hit" {
			found = true
			if category != protocol.SoundCategoryHostile {
				t.Errorf("the thorns sound played in category %d, want the hostile one", category)
			}
		}
		return found
	})
	if !found {
		t.Error("no thorns hit sound reached the wearer")
	}
}

// TestThornsHurtsAProjectileShooter checks retaliation against the owner,
// rather than the projectile, and the unenchanted equipment control.
func TestThornsHurtsAProjectileShooter(t *testing.T) {
	for _, projectile := range []string{"arrow", "trident", "mob arrow"} {
		for _, enchanted := range []bool{false, true} {
			t.Run(fmt.Sprintf("%s/thorns=%v", projectile, enchanted), func(t *testing.T) {
				t.Parallel()
				srv, connect := servertest.Start(t, servertest.SurvivalConfig())
				victim := connect()
				victimID := victim.JoinAs("Knight")
				shooter := connect()
				shooterID := shooter.JoinAs("Archer")
				victim.MoveToGround(4.5, 64, 0.5, true)
				shooter.MoveToGround(0.5, 64, 0.5, true)
				victim.Settle()
				shooter.Settle()
				servertest.HoldTicks(t, srv)
				thornsWearer(t, srv, "Knight")
				srv.GiveSlot("Knight", player.OffHandSlot, protocol.ItemStack{})
				if enchanted {
					srv.GiveSlot("Knight", player.ArmorSlot+1, servertest.EnchantedStack(t, "minecraft:diamond_chestplate", "minecraft:thorns", 3))
				}
				if projectile == "mob arrow" {
					shooterID = srv.SpawnMob("minecraft:cow", 0.5, 64, 4.5)
				}
				thornsID := registry.MustID("minecraft:damage_type", "minecraft:thorns")
				answered := false
				landed := 0
				for shots := 0; shots < 40 && !answered; shots++ {
					victim.MoveToGround(4.5, 64, 0.5, true)
					victim.Settle()
					// Heal between shots so all forty can land on the same wearer.
					srv.SetHealth("Knight", 20)
					switch projectile {
					case "arrow":
						srv.ShootPlayerArrow("Archer", 1.5, 65, 0.5, 2, 0, 0)
					case "mob arrow":
						srv.ShootMobArrow(shooterID, 1.5, 65, 0.5, 2, 0, 0)
					case "trident":
						giveTrident(srv, "Archer")
						startTridentCharge(t, srv, shooter, "Archer", -90, 0, 12)
						endTridentCharge(t, srv, shooter, "Archer")
					}
					victim.PumpTicks(srv, 14, func(id int32, data []byte) bool {
						if id == protocol.IDPlayDamageEvent {
							r := protocol.NewReader(data)
							entityID, damageType := r.ReadVarInt(), r.ReadVarInt()
							if entityID == victimID {
								landed++
							}
							if entityID == shooterID {
								if damageType != thornsID {
									t.Errorf("shooter damage type = %d, want thorns %d", damageType, thornsID)
								}
								answered = true
							}
						}
						return false
					})
				}
				if landed == 0 {
					t.Fatal("no projectile hurt the wearer")
				}
				if !enchanted && landed != 40 {
					t.Errorf("unenchanted control landed %d hits, want 40", landed)
				}
				if !enchanted && srv.PlayerHealth("Archer") != 20 {
					t.Errorf("unenchanted wearer hurt the shooter: health %v", srv.PlayerHealth("Archer"))
				}
				if answered != enchanted {
					t.Errorf("shooter hurt = %v, want %v", answered, enchanted)
				}
			})
		}
	}
}

// TestAThornsKillCreditsTheWearerNotAnEarlierHitter has a creative player,
// whom the zombie never targets, punch it first; the Thorns III wearer's
// retaliation then kills it, and mob_kills goes to the wearer alone.
// vanilla: LivingEntity.resolvePlayerResponsibleForDamage
func TestAThornsKillCreditsTheWearerNotAnEarlierHitter(t *testing.T) {
	t.Parallel()
	srv, c := servertest.Join(t, servertest.HostileTestConfig(), "Knight")
	hitter := servertest.DialServer(t, srv).Join("Hitter")
	srv.SetPlayerGameMode("Hitter", 1)
	hitter.MoveToGround(4.5, 64, 6.5, true)
	hitter.Settle()
	servertest.NightWorld(srv, c)
	thornsWearer(t, srv, "Knight")

	servertest.HoldTicks(t, srv)
	zombieID := srv.SpawnMob("minecraft:zombie", 4.5, 64, 4.5)
	servertest.AwaitFullSwing(t, srv, "Hitter")
	hitter.Attack(zombieID)
	// Read before the ticks are counted: a held loop outruns the connection.
	hitter.Settle()
	servertest.UntilTicks(t, srv, 20, "the creative punch lands", func() bool {
		h, _ := srv.MobHealth(zombieID)
		return h < 20
	})
	if !srv.SetMobHealth(zombieID, 0.5) {
		t.Fatal("the zombie is not in the herd")
	}
	stat := func(name string) int32 { return srv.StatValue(name, registry.StatCustom, "minecraft:mob_kills") }
	servertest.UntilTicks(t, srv, 2400, "the thorns kill the zombie", func() bool {
		_, alive := srv.MobHealth(zombieID)
		return !alive
	})
	if knight, hit := stat("Knight"), stat("Hitter"); knight != 1 || hit != 0 {
		t.Errorf("mob_kills after the thorns kill: wearer %d, earlier hitter %d; want 1 and 0", knight, hit)
	}
}

// TestThornsHurtsTheOwnerOfANonArrowProjectile checks that a fireball, a
// wither skull and evoker fangs that hurt a Thorns wearer are answered on
// their owner, and that each answer wears the chestplate by two.
func TestThornsHurtsTheOwnerOfANonArrowProjectile(t *testing.T) {
	for _, kind := range []string{"small fireball", "wither skull", "fangs"} {
		t.Run(kind, func(t *testing.T) {
			t.Parallel()
			srv, connect := servertest.Start(t, servertest.SurvivalConfig())
			victim := connect()
			victimID := victim.JoinAs("Knight")
			victim.MoveToGround(4.5, 64, 0.5, true)
			victim.Settle()
			servertest.HoldTicks(t, srv)
			thornsWearer(t, srv, "Knight")
			ownerID := srv.SpawnMob("minecraft:cow", 0.5, 64, 4.5)
			thornsID := registry.MustID("minecraft:damage_type", "minecraft:thorns")
			landed, answers := 0, 0
			for round := 0; round < 40 && answers == 0; round++ {
				victim.MoveToGround(4.5, 64, 0.5, true)
				victim.Settle()
				srv.SetHealth("Knight", 20)
				switch kind {
				case "small fireball":
					srv.LaunchFireball(entity.ThrownSmallFireball, 1.5, 65, 0.5, 1, 0, 0, ownerID, 1)
				case "wither skull":
					srv.LaunchFireball(entity.ThrownWitherSkull, 1.5, 65, 0.5, 1, 0, 0, ownerID, 1)
				case "fangs":
					srv.SpawnFangs(4.5, 64, 0.5, ownerID)
				}
				victim.PumpTicks(srv, 14, func(id int32, data []byte) bool {
					if id == protocol.IDPlayDamageEvent {
						r := protocol.NewReader(data)
						entityID, damageType := r.ReadVarInt(), r.ReadVarInt()
						if entityID == victimID {
							landed++
						}
						if entityID == ownerID {
							if damageType != thornsID {
								t.Errorf("owner damage type = %d, want thorns %d", damageType, thornsID)
							}
							answers++
						}
					}
					return false
				})
			}
			if landed == 0 {
				t.Fatal("nothing hurt the wearer")
			}
			if answers == 0 {
				t.Fatal("the owner was never answered with thorns")
			}
			if wear := srv.InventorySlot("Knight", player.OffHandSlot).Damage; int(wear) != 2*answers {
				t.Errorf("chestplate wear = %d, want %d", wear, 2*answers)
			}
		})
	}
}
