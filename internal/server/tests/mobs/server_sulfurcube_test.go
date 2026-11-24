package mobs

import (
	"math"
	"testing"

	"github.com/golem-mc/golem/internal/config"
	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/internal/server"
	"github.com/golem-mc/golem/internal/server/player"
	"github.com/golem-mc/golem/internal/server/servertest"
	"github.com/golem-mc/golem/internal/server/world"
	"github.com/golem-mc/golem/nbt"
	"github.com/golem-mc/golem/registry"
)

func TestSulfurCubeBucketReleasesWithoutWaterAndAwardsUsed(t *testing.T) {
	t.Parallel()
	srv, c := squidTests(t)
	// The player's dry ledge, away from the pond, keeps this a fluid-free release.
	c.MoveTo(playerX, 64, playerZ, 0, 90)
	c.Settle()
	bucketID := servertest.MustItemID(t, "minecraft:sulfur_cube_bucket")
	bucket := protocol.ItemStack{ItemID: bucketID, Count: 1}
	bucket.SetBucketEntityData(nbt.Compound{"age": nbt.Int(0), "age_locked": nbt.Byte(1), "Health": nbt.Float(3), "NoAI": nbt.Byte(1)})
	srv.GiveSlot("Striker", player.HotbarSlot, bucket)
	c.Settle()
	servertest.UseHeld(c, 0, 90)
	c.Settle()
	var id int32
	for _, m := range srv.Mobs() {
		if m.Kind == "minecraft:sulfur_cube" {
			id = m.ID
		}
	}
	if id == 0 {
		t.Fatal("bucket released no sulfur cube")
	}
	if got := srv.StatValue("Striker", registry.StatUsed, "minecraft:sulfur_cube_bucket"); got != 1 {
		t.Fatalf("bucket used = %d, want 1", got)
	}
	if hp, _ := srv.MobHealth(id); hp != 3 {
		t.Fatalf("released health = %g", hp)
	}
	if got := srv.BlockAt(8, 64, -3); got != registry.MustState("minecraft:air", nil) {
		t.Fatalf("bucket placed a block: %d", got)
	}
	stone := servertest.MustItemID(t, "minecraft:stone")
	srv.GiveSlot("Striker", player.HotbarSlot, protocol.ItemStack{ItemID: stone, Count: 3})
	c.UseOn(id)
	c.Settle()
	if got := srv.InventorySlot("Striker", player.HotbarSlot); got.Count != 2 {
		t.Fatalf("absorption consumed wrong count: %+v", got)
	}
	empty := servertest.MustItemID(t, "minecraft:bucket")
	srv.GiveSlot("Striker", player.HotbarSlot, protocol.ItemStack{ItemID: empty, Count: 1})
	// Bucketable pickup consumes no item-use statistic, unlike BucketItem.use.
	c.UseOn(id)
	c.Settle()
	held := srv.InventorySlot("Striker", player.HotbarSlot)
	if held.ItemID != bucketID {
		t.Fatalf("scooped stack = %+v", held)
	}
	content, ok := held.SulfurCubeContent()
	if !ok || content.ItemID != stone || content.Count != 1 {
		t.Fatalf("bucket lost absorbed block: %+v", content)
	}
	tag := held.BucketEntityData()
	age, _ := nbt.IntField(tag, "age")
	if age != 0 || !nbt.BoolField(tag, "age_locked") {
		t.Fatalf("bucket lost age: %+v", tag)
	}
	servertest.UseHeld(c, 0, 90)
	c.Settle()
	for _, m := range srv.Mobs() {
		if m.Kind == "minecraft:sulfur_cube" && m.ID != id {
			id = m.ID
			break
		}
	}
	shears := servertest.MustItemID(t, "minecraft:shears")
	srv.GiveSlot("Striker", player.HotbarSlot, protocol.ItemStack{ItemID: shears, Count: 1})
	c.UseOn(id)
	c.Settle()
	if got := srv.InventorySlot("Striker", player.HotbarSlot); got.Damage != 1 {
		t.Fatalf("shears wear = %d", got.Damage)
	}
	found := false
	for _, item := range srv.ItemsInDimension("minecraft:overworld") {
		found = found || item.Item == "minecraft:stone"
	}
	if !found {
		t.Fatal("shearing lost absorbed stone")
	}
	if got := srv.StatValue("Striker", registry.StatUsed, "minecraft:bucket"); got != 0 {
		t.Fatalf("pickup used = %d, want 0", got)
	}
}

// sulfurExplosionFixture uses only flat chunks around the player, without a pond.
func sulfurExplosionFixture(t *testing.T) (*server.Server, *servertest.Client, int32) {
	t.Helper()
	cfg := servertest.MobTestConfig()
	cfg.ViewDistance, cfg.SimulationDistance = config.MinDistance, config.MinDistance
	srv, c := servertest.Join(t, cfg, "Striker")
	c.MoveToGround(playerX, 64, playerZ, true)
	c.Settle()
	srv.StopTick()
	id := sulfurCubeWith(t, srv, c, "minecraft:tnt")
	srv.SetMobNoAI(id)
	servertest.HoldTicks(t, srv)
	return srv, c, id
}

func TestExplosiveSulfurCubeFuseAndFlintAndSteel(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name       string
		grief, tnt bool
	}{
		{"blast", true, true}, {"mob_griefing_off", false, true}, {"tnt_explodes_off_after_priming", true, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			srv, c, id := sulfurExplosionFixture(t)
			srv.SetGameRule(world.RuleMobGriefing.String(), tc.grief)
			victim := srv.SpawnMob("minecraft:cow", playerX+3, 64, playerZ)
			srv.SetMobNoAI(victim)
			before, _ := srv.MobHealth(victim)
			// Dirt immediately beneath the blast proves block destruction independently
			// of entity damage, without an enclosure shielding the victim.
			dirt := registry.MustState("minecraft:dirt", nil)
			if !srv.SetBlock(9, 63, -3, dirt) {
				t.Fatal("blast floor not loaded")
			}
			flint := servertest.MustItemID(t, "minecraft:flint_and_steel")
			srv.GiveSlot("Striker", player.HotbarSlot, protocol.ItemStack{ItemID: flint, Count: 1})
			c.UseOn(id)
			c.Settle()
			if got := srv.StatValue("Striker", registry.StatUsed, "minecraft:flint_and_steel"); got != 1 {
				t.Fatalf("flint and steel used = %d", got)
			}
			if got := srv.InventorySlot("Striker", player.HotbarSlot); got.Damage != 1 {
				t.Fatalf("flint and steel wear = %d", got.Damage)
			}
			srv.SetGameRule(world.RuleTNTExplodes.String(), tc.tnt)
			c.MoveToGround(playerX-8, 64, playerZ, true)
			c.Settle()
			servertest.RunTicks(t, srv, 119)
			if _, ok := srv.MobHealth(id); !ok {
				t.Fatal("cube blew before its 120-tick fuse")
			}
			servertest.RunTicks(t, srv, 1)
			if _, ok := srv.MobHealth(id); ok {
				t.Fatal("primed cube survived tick 120")
			}
			after, _ := srv.MobHealth(victim)
			if damaged := after < before; damaged != tc.tnt {
				t.Fatalf("blast victim health %g -> %g, TNT enabled=%v", before, after, tc.tnt)
			}
			if broken := srv.BlockAt(9, 63, -3) != dirt; broken != (tc.tnt && tc.grief) {
				t.Fatalf("blast floor broken=%v, TNT=%v griefing=%v", broken, tc.tnt, tc.grief)
			}
		})
	}
}

func TestExplosiveSulfurCubeFireExplosionAndPowerPriming(t *testing.T) {
	t.Parallel()
	for _, trigger := range []string{"fire", "explosion", "power"} {
		t.Run(trigger, func(t *testing.T) {
			srv, c, id := sulfurExplosionFixture(t)
			switch trigger {
			case "fire":
				srv.HurtMobByWorld(id, 1, "minecraft:in_fire")
			case "explosion":
				srv.HurtMobByWorld(id, 1, "minecraft:explosion")
			case "power":
				if !srv.SetBlock(10, 64, -3, registry.MustState("minecraft:redstone_block", nil)) {
					t.Fatal("powered neighbor not loaded")
				}
			}
			c.MoveToGround(playerX-8, 64, playerZ, true)
			c.Settle()
			servertest.RunTicks(t, srv, 1)
			fuse := int32(-1)
			c.SettleAndCollect(func(packetID int32, data []byte) {
				if packetID != protocol.IDPlayEntityMetadata {
					return
				}
				r := protocol.NewReader(data)
				if r.ReadVarInt() != id {
					return
				}
				entries, ok := servertest.ReadMetadata(r)
				if !ok {
					t.Fatal("invalid cube metadata")
				}
				for _, e := range entries {
					if e.Index == protocol.MetadataIndexSulfurCubeMaxFuse {
						fuse = e.VarintVal
					}
				}
			})
			if trigger == "explosion" {
				if fuse < 15 || fuse > 44 {
					t.Fatalf("explosion fuse = %d, want 15..44", fuse)
				}
			} else if fuse != 120 {
				t.Fatalf("%s fuse = %d, want 120", trigger, fuse)
			}
			// Power primes during the first tick; damage primes before that tick.
			remaining := int(fuse) - 1
			if trigger == "power" {
				remaining++
			}
			servertest.RunTicks(t, srv, remaining-1)
			if _, ok := srv.MobHealth(id); !ok {
				t.Fatal("trigger fuse ended early")
			}
			servertest.RunTicks(t, srv, 1)
			if _, ok := srv.MobHealth(id); ok {
				t.Fatal("trigger fuse failed to explode")
			}
		})
	}
}

// sulfurCubeWith spawns an adult cube beside the player and feeds it one item.
func sulfurCubeWith(t *testing.T, srv *server.Server, c *servertest.Client, item string) int32 {
	t.Helper()
	id := srv.SpawnMob("minecraft:sulfur_cube", playerX+1, 64, playerZ)
	srv.GiveSlot("Striker", player.HotbarSlot, protocol.ItemStack{ItemID: servertest.MustItemID(t, item), Count: 2})
	c.UseOn(id)
	c.Settle()
	if got := srv.MobEquipment(id, "body"); got.Empty() {
		t.Fatalf("cube swallowed no %s", item)
	}
	return id
}

func TestSulfurCubeBounceFollowsArchetype(t *testing.T) {
	t.Parallel()
	srv, c := squidTests(t)
	bounces := func(item string) bool {
		id := sulfurCubeWith(t, srv, c, item)
		_, ground, _, _ := srv.MobPos(id)
		srv.PushMob(id, 0, 0.5, 0)
		servertest.HoldTicks(t, srv)
		landed := false
		for range 60 {
			servertest.RunTicks(t, srv, 1)
			_, y, _, _ := srv.MobPos(id)
			if y <= ground+1e-3 {
				landed = true
			} else if landed && y > ground+0.2 {
				return true
			}
		}
		return false
	}
	if !bounces("minecraft:oak_planks") {
		t.Fatal("a bouncy cube did not rebound off the ground")
	}
	if bounces("minecraft:honeycomb_block") {
		t.Fatal("a sticky cube rebounded")
	}
}

func TestFilledSulfurCubeShrugsOffMeleeButTakesTheShove(t *testing.T) {
	t.Parallel()
	srv, c := squidTests(t)
	id := sulfurCubeWith(t, srv, c, "minecraft:stone")
	before, _ := srv.MobHealth(id)
	servertest.HoldTicks(t, srv)
	c.Attack(id)
	servertest.RunTicks(t, srv, 2)
	if after, _ := srv.MobHealth(id); after != before {
		t.Fatalf("melee hurt a block-holding cube: %g -> %g", before, after)
	}
	vx, _, vz, _ := srv.MobVelocity(id)
	if vx == 0 && vz == 0 {
		t.Fatal("melee gave the cube no shove")
	}
}

func TestHotSulfurCubeBurnsAPlayerItPushes(t *testing.T) {
	t.Parallel()
	srv, c := squidTests(t)
	id := sulfurCubeWith(t, srv, c, "minecraft:magma_block")
	srv.MoveMob(id, playerX+0.3, playerZ)
	servertest.HoldTicks(t, srv)
	servertest.RunTicks(t, srv, 5)
	if hp := srv.PlayerHealth("Striker"); hp >= 20 {
		t.Fatalf("a hot cube left the player at %g health", hp)
	}
}

// TestFilledSulfurCubeKnockbackPitchAndDamageCommand checks that a filled
// sulfur cube bends knockback by the attacker's pitch on melee hits and by the
// causing entity's eye and look on /damage ... by <entity>.
// vanilla: SulfurCube.knockback, DamageCommand.damage
func TestFilledSulfurCubeKnockbackPitchAndDamageCommand(t *testing.T) {
	t.Parallel()

	t.Run("melee pitch bends vertical knockback", func(t *testing.T) {
		t.Parallel()
		srv, c := squidTests(t)
		servertest.HoldTicks(t, srv)
		c.MoveTo(0.5, 64, 10.3, 0, 30)
		c.ChatCommand(`summon minecraft:sulfur_cube 0.5 64 12.5 {NoAI:1b,Age:0,equipment:{body:{id:"minecraft:tnt",count:1}}}`)
		c.Settle()
		ids := srv.MobIDsOfKind("minecraft:sulfur_cube")
		if len(ids) != 1 {
			t.Fatalf("expected 1 sulfur cube, got %d", len(ids))
		}
		// Let the bare-hand attack cooldown reach full strength (1.0 damage).
		servertest.RunTicks(t, srv, 10)
		c.Attack(ids[0])
		c.Settle()
		vx, vy, vz, ok := srv.MobVelocity(ids[0])
		if !ok {
			t.Fatal("sulfur cube disappeared")
		}
		if math.Abs(vy-0.14906) > 0.001 {
			t.Fatalf("melee Motion = [%g, %g, %g], want vy ≈ 0.149 (+-0.001)", vx, vy, vz)
		}
		if math.Abs(vx) > 0.001 || math.Abs(vz-0.33) > 0.001 {
			t.Fatalf("melee Motion = [%g, %g, %g], want [0, 0.149, 0.33] (+-0.001)", vx, vy, vz)
		}
	})

	t.Run("damage command by player bends knockback", func(t *testing.T) {
		t.Parallel()
		srv, c := squidTests(t)
		servertest.HoldTicks(t, srv)
		c.ChatCommand(`summon minecraft:sulfur_cube -15 64 20 {NoAI:1b,Age:0,Tags:["d"],equipment:{body:{id:"minecraft:tnt",count:1}}}`)
		c.Settle()
		ids := srv.MobIDsOfKind("minecraft:sulfur_cube")
		if len(ids) != 1 {
			t.Fatalf("expected 1 sulfur cube, got %d", len(ids))
		}
		servertest.RunTicks(t, srv, 1)
		c.MoveTo(-15.3, 64, 17.5, 15, 10)
		c.ChatCommand(`damage @e[tag=d,limit=1] 4 minecraft:player_attack by @s`)
		c.Settle()
		vx, vy, vz, ok := srv.MobVelocity(ids[0])
		if !ok {
			t.Fatal("sulfur cube disappeared")
		}
		if math.Abs(vx-0.5871) > 0.001 || math.Abs(vy-0.144) > 0.001 || math.Abs(vz-0.3015) > 0.001 {
			t.Fatalf("/damage Motion = [%.4f, %.4f, %.4f], want [0.5871, 0.144, 0.3015] (+-0.001)", vx, vy, vz)
		}
	})

	t.Run("damage command by mob bends knockback", func(t *testing.T) {
		t.Parallel()
		srv, c := squidTests(t)
		servertest.HoldTicks(t, srv)
		c.ChatCommand(`summon minecraft:sulfur_cube -15 64 20 {NoAI:1b,Age:0,Tags:["d"],equipment:{body:{id:"minecraft:tnt",count:1}}}`)
		c.ChatCommand(`summon minecraft:zombie -15.3 64 17.5 {NoAI:1b,Tags:["a"],Rotation:[15f,10f]}`)
		c.Settle()
		ids := srv.MobIDsOfKind("minecraft:sulfur_cube")
		if len(ids) != 1 {
			t.Fatalf("expected 1 sulfur cube, got %d", len(ids))
		}
		servertest.RunTicks(t, srv, 1)
		c.ChatCommand(`damage @e[tag=d,limit=1] 4 minecraft:mob_attack by @e[tag=a,limit=1]`)
		c.Settle()
		vx, vy, vz, ok := srv.MobVelocity(ids[0])
		if !ok {
			t.Fatal("sulfur cube disappeared")
		}
		if math.Abs(vx-0.5871) > 0.001 || math.Abs(vy-0.144) > 0.001 || math.Abs(vz-0.3015) > 0.001 {
			t.Fatalf("/damage by mob Motion = [%.4f, %.4f, %.4f], want [0.5871, 0.144, 0.3015] (+-0.001)", vx, vy, vz)
		}
	})

	t.Run("damage command without by uses plain knockback", func(t *testing.T) {
		t.Parallel()
		srv, c := squidTests(t)
		servertest.HoldTicks(t, srv)
		c.ChatCommand(`summon minecraft:sulfur_cube -15 64 20 {NoAI:1b,Age:0,Tags:["d"],equipment:{body:{id:"minecraft:tnt",count:1}}}`)
		c.Settle()
		ids := srv.MobIDsOfKind("minecraft:sulfur_cube")
		if len(ids) != 1 {
			t.Fatalf("expected 1 sulfur cube, got %d", len(ids))
		}
		servertest.RunTicks(t, srv, 1)
		c.ChatCommand(`damage @e[tag=d,limit=1] 4 minecraft:player_attack at -15.3 64 17.5`)
		c.Settle()
		vx, vy, vz, ok := srv.MobVelocity(ids[0])
		if !ok {
			t.Fatal("sulfur cube disappeared")
		}
		if vy != 0 || math.Hypot(vx, vz) == 0 {
			t.Fatalf("/damage at Motion = [%.4f, %.4f, %.4f], want plain horizontal knockback with vy == 0", vx, vy, vz)
		}
	})
}
