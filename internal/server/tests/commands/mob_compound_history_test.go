package commands

import (
	"slices"
	"strings"
	"testing"

	"github.com/golem-mc/golem/internal/config"
	"github.com/golem-mc/golem/internal/server/servertest"
)

// Recorded HashMap iteration of the supported vanilla 26.3 save writer fields.
// Entity, LivingEntity, Mob, AgeableMob, Animal, Pig, Sheep, Chicken,
// each fixture's addAdditionalSaveData (including inherited writers).
var mobCompoundHistory = []struct{ kind, keys string }{
	{"pig", "NoGravity AgeLocked Brain Invulnerable FallFlying ForcedAge sound_variant PortalCooldown AbsorptionAmount InLove DeathTime variant PersistenceRequired UUID Tags Age Motion Health equipment LeftHanded fall_distance Air OnGround NoAI Rotation current_impulse_context_reset_grace_time Pos Fire CanPickUpLoot attributes HurtTime"},
	{"sheep", "NoGravity AgeLocked Brain Invulnerable FallFlying ForcedAge PortalCooldown AbsorptionAmount InLove DeathTime PersistenceRequired UUID Tags Age Motion Health Color equipment LeftHanded fall_distance Air OnGround NoAI Rotation current_impulse_context_reset_grace_time Pos Fire CanPickUpLoot attributes Sheared HurtTime"},
	{"chicken", "NoGravity AgeLocked IsChickenJockey Brain Invulnerable FallFlying ForcedAge sound_variant PortalCooldown AbsorptionAmount InLove DeathTime variant PersistenceRequired UUID Tags Age Motion Health equipment LeftHanded fall_distance Air OnGround NoAI Rotation current_impulse_context_reset_grace_time EggLayTime Pos Fire CanPickUpLoot attributes HurtTime"},
	{"creeper", "NoGravity Brain powered Invulnerable FallFlying PortalCooldown AbsorptionAmount DeathTime PersistenceRequired UUID Tags Fuse Motion Health equipment LeftHanded fall_distance Air OnGround ignited NoAI Rotation current_impulse_context_reset_grace_time Pos Fire CanPickUpLoot attributes HurtTime ExplosionRadius"},
	{"skeleton", "NoGravity Brain Invulnerable FallFlying FreezingTime PortalCooldown AbsorptionAmount DeathTime StrayConversionTime PersistenceRequired UUID Tags Motion Health equipment LeftHanded fall_distance Air OnGround NoAI Rotation current_impulse_context_reset_grace_time Pos Fire CanPickUpLoot attributes HurtTime"},
	{"bat", "NoGravity Brain Invulnerable FallFlying PortalCooldown AbsorptionAmount DeathTime PersistenceRequired BatFlags UUID Tags Motion Health equipment LeftHanded fall_distance Air OnGround NoAI Rotation current_impulse_context_reset_grace_time Pos Fire CanPickUpLoot attributes HurtTime"},
	{"ghast", "NoGravity Brain Invulnerable FallFlying PortalCooldown AbsorptionAmount DeathTime PersistenceRequired UUID Tags Motion Health equipment LeftHanded fall_distance Air OnGround ExplosionPower NoAI Rotation current_impulse_context_reset_grace_time Pos Fire CanPickUpLoot attributes HurtTime"},
	{"panda", "NoGravity AgeLocked Brain Invulnerable FallFlying ForcedAge PortalCooldown AbsorptionAmount InLove DeathTime HiddenGene PersistenceRequired UUID MainGene Tags Age Motion Health equipment LeftHanded fall_distance Air OnGround NoAI Rotation current_impulse_context_reset_grace_time Pos Fire CanPickUpLoot attributes HurtTime"},
	{"fox", "NoGravity AgeLocked Brain Crouching Sitting Invulnerable FallFlying ForcedAge PortalCooldown AbsorptionAmount InLove DeathTime PersistenceRequired UUID Tags Age Motion Health equipment LeftHanded fall_distance Air OnGround NoAI Rotation Trusted current_impulse_context_reset_grace_time Type Sleeping Pos Fire CanPickUpLoot attributes HurtTime"},
	{"wolf", "NoGravity AgeLocked Brain Sitting Invulnerable FallFlying ForcedAge sound_variant PortalCooldown AbsorptionAmount InLove anger_end_time DeathTime variant PersistenceRequired UUID Tags Age CollarColor Motion Health equipment LeftHanded fall_distance Air OnGround NoAI Rotation current_impulse_context_reset_grace_time Pos Fire CanPickUpLoot attributes HurtTime"},
	{"horse", "NoGravity AgeLocked Brain Tame Invulnerable FallFlying ForcedAge PortalCooldown AbsorptionAmount Bred InLove EatingHaystack DeathTime PersistenceRequired UUID Tags Age Motion Health equipment LeftHanded fall_distance Air OnGround NoAI Rotation current_impulse_context_reset_grace_time Variant Pos Fire Temper CanPickUpLoot attributes HurtTime"},
	{"llama", "NoGravity AgeLocked Brain Tame Invulnerable FallFlying ForcedAge PortalCooldown AbsorptionAmount Bred InLove EatingHaystack DeathTime PersistenceRequired UUID Tags Age Motion Health equipment LeftHanded fall_distance Air OnGround NoAI Rotation current_impulse_context_reset_grace_time Variant Pos ChestedHorse Fire Temper CanPickUpLoot attributes HurtTime Strength"},
	{"turtle", "NoGravity AgeLocked Brain Invulnerable FallFlying ForcedAge PortalCooldown AbsorptionAmount InLove DeathTime PersistenceRequired UUID Tags Age Motion Health equipment LeftHanded fall_distance Air OnGround NoAI Rotation current_impulse_context_reset_grace_time has_egg Pos Fire CanPickUpLoot attributes HurtTime home_pos"},
	{"tadpole", "NoGravity AgeLocked Brain Invulnerable FallFlying PortalCooldown AbsorptionAmount DeathTime PersistenceRequired UUID Tags Age Motion FromBucket Health equipment LeftHanded fall_distance Air OnGround NoAI Rotation current_impulse_context_reset_grace_time Pos Fire CanPickUpLoot attributes HurtTime"},
	{"zombie", "NoGravity Brain IsBaby Invulnerable FallFlying PortalCooldown AbsorptionAmount InWaterTime DeathTime PersistenceRequired UUID Tags Motion Health equipment LeftHanded fall_distance Air OnGround NoAI Rotation current_impulse_context_reset_grace_time Pos CanBreakDoors Fire CanPickUpLoot attributes HurtTime DrownedConversionTime"},
	{"zombie_villager", "NoGravity Brain IsBaby Invulnerable FallFlying Gossips PortalCooldown AbsorptionAmount InWaterTime DeathTime Xp PersistenceRequired UUID Tags ConversionTime Motion Health equipment LeftHanded fall_distance Air OnGround NoAI Rotation current_impulse_context_reset_grace_time Pos CanBreakDoors Fire CanPickUpLoot VillagerData attributes VillagerDataFinalized HurtTime DrownedConversionTime"},
	{"allay", "NoGravity Brain listener Invulnerable FallFlying PortalCooldown AbsorptionAmount DuplicationCooldown DeathTime PersistenceRequired UUID Tags Motion Health equipment LeftHanded fall_distance Air OnGround NoAI Rotation current_impulse_context_reset_grace_time Pos Fire CanPickUpLoot attributes HurtTime Inventory"},
	{"armadillo", "NoGravity AgeLocked Brain Invulnerable FallFlying ForcedAge PortalCooldown AbsorptionAmount InLove DeathTime PersistenceRequired state UUID Tags Age Motion Health equipment LeftHanded fall_distance Air OnGround NoAI Rotation scute_time current_impulse_context_reset_grace_time Pos Fire CanPickUpLoot attributes HurtTime"},
	{"ravager", "NoGravity Brain CanJoinRaid PatrolLeader Invulnerable FallFlying PortalCooldown AbsorptionAmount AttackTick DeathTime PersistenceRequired UUID Patrolling Tags Motion RoarTick Health StunTick equipment LeftHanded fall_distance Air OnGround NoAI Rotation current_impulse_context_reset_grace_time Wave Pos Fire CanPickUpLoot attributes HurtTime"},
	{"enderman", "NoGravity Brain Invulnerable FallFlying PortalCooldown AbsorptionAmount anger_end_time DeathTime PersistenceRequired UUID Tags Motion Health equipment LeftHanded fall_distance Air OnGround NoAI Rotation current_impulse_context_reset_grace_time Pos Fire CanPickUpLoot attributes HurtTime"},
	{"shulker", "NoGravity Brain Invulnerable FallFlying PortalCooldown AbsorptionAmount DeathTime AttachFace PersistenceRequired UUID Tags Peek Motion Health Color equipment LeftHanded fall_distance Air OnGround NoAI Rotation current_impulse_context_reset_grace_time Pos Fire CanPickUpLoot attributes HurtTime"},
}

func expectMobHistory(t *testing.T, c *servertest.Client, kind, keys string) {
	t.Helper()
	expectNonlivingHistory(t, c, kind, keys)
	selector := "@e[type=" + kind + ",limit=1]"
	for _, cmd := range []string{
		"data get entity " + selector + " equipment",
		`tellraw @s {nbt:"equipment",entity:"` + selector + `"}`,
		`tellraw @s {nbt:"equipment",entity:"` + selector + `",plain:1b}`,
	} {
		c.ChatCommand(cmd)
		line := servertest.FeedbackComponent(t, c)
		text := line.Plain()
		if strings.HasPrefix(cmd, "data ") {
			text = line.With[1].Plain()
		}
		if got := rootCompoundKeys(text); !slices.Equal(got, []string{"mainhand", "head"}) {
			t.Errorf("%s: keys = %v", cmd, got)
		}
	}
}

func TestMobCompoundInsertionHistory(t *testing.T) {
	t.Parallel()
	cfg := servertest.BlockTestConfig()
	cfg.LevelName = t.TempDir()
	cfg.ViewDistance, cfg.SimulationDistance = config.MinDistance, config.MinDistance
	srv, c := servertest.Join(t, cfg, "Alice", servertest.OpsListing(t, "Alice"))
	servertest.HoldTicks(t, srv)
	for _, fixture := range mobCompoundHistory {
		t.Run(fixture.kind, func(t *testing.T) {
			c.ChatCommand("summon " + fixture.kind + ` 4.5 70 4.5 {NoGravity:1b,NoAI:1b,Tags:["order"],attributes:[{id:"minecraft:scale",base:1.25d}],equipment:{mainhand:{id:"minecraft:stone",count:1},head:{id:"minecraft:leather_helmet",count:1}}}`)
			servertest.AwaitSystemChatSaying(t, c, "commands.summon.success")
			c.Settle()
			expectMobHistory(t, c, fixture.kind, fixture.keys)
			c.ChatCommand("data merge entity @e[type=" + fixture.kind + `,limit=1] {Health:2f,Tags:["order","written"]}`)
			servertest.AwaitSystemChatSaying(t, c, "commands.data.entity.modified")
			c.Settle()
			expectMobHistory(t, c, fixture.kind, fixture.keys)
			expectSavedScoreboardTag(t, c, "@e[type="+fixture.kind+",limit=1]", "written")
		})
	}
	c.ChatCommand(`data modify storage order text set string entity @e[type=pig,limit=1]`)
	line := servertest.FeedbackComponent(t, c)
	if len(line.Extra) == 1 {
		line = line.Extra[0]
	}
	if line.Translate != "commands.data.modify.expected_value" || len(line.With) != 1 {
		t.Fatalf("compact SNBT = %+v", line)
	}
	if keys := rootCompoundKeys(line.With[0].Plain()); !slices.IsSorted(keys) {
		t.Errorf("compact SNBT keys = %v", keys)
	}
	srv.FlushMobs()
	c.Disconnect()
	srv.Shutdown()
	again, connect := servertest.Start(t, cfg, servertest.OpsListing(t, "Alice"))
	servertest.FreezeUntilHeld(t, again)
	client := connect().Join("Alice")
	servertest.HoldTicks(t, again)
	servertest.UntilTicks(t, again, 20, "saved mobs wake", func() bool {
		for _, fixture := range mobCompoundHistory {
			if len(again.SelectedEntities("minecraft:"+fixture.kind)) != 1 {
				return false
			}
		}
		return true
	})
	client.Settle()
	for _, fixture := range mobCompoundHistory {
		expectMobHistory(t, client, fixture.kind, fixture.keys)
		expectSavedScoreboardTag(t, client, "@e[type="+fixture.kind+",limit=1]", "written")
	}
}

// The 24-key saveWithoutId fits a 32-bucket table; inserting id first grows it.
// vanilla: Entity.saveWithoutId, NbtPredicate.getEntityTagToCompare
func TestMobQueryAvoidsSaveIdGrowth(t *testing.T) {
	t.Parallel()
	cfg := servertest.BlockTestConfig()
	cfg.ViewDistance, cfg.SimulationDistance = config.MinDistance, config.MinDistance
	srv, c := servertest.Join(t, cfg, "Alice", servertest.OpsListing(t, "Alice"))
	servertest.HoldTicks(t, srv)
	c.ChatCommand(`summon bat 4.5 70 4.5 {NoGravity:1b,NoAI:1b,attributes:[{id:"minecraft:scale",base:1.25d}]}`)
	servertest.AwaitSystemChatSaying(t, c, "commands.summon.success")
	c.Settle()
	const keys = "NoGravity Motion Brain Health Invulnerable FallFlying LeftHanded fall_distance Air OnGround PortalCooldown AbsorptionAmount NoAI Rotation current_impulse_context_reset_grace_time Pos DeathTime Fire CanPickUpLoot attributes PersistenceRequired BatFlags UUID HurtTime"
	expectNonlivingHistory(t, c, "bat", keys)
	c.ChatCommand(`data merge entity @e[type=bat,limit=1] {Health:2f}`)
	servertest.AwaitSystemChatSaying(t, c, "commands.data.entity.modified")
	c.Settle()
	expectNonlivingHistory(t, c, "bat", keys)
	c.ChatCommand("data get entity @e[type=bat,limit=1] id")
	c.ExpectCommandFailure(servertest.CommandErrorMessageForTest("arguments.nbtpath.nothing_found", "id"))
}

// Native subclass fields must enter after the living timers and mob flags,
// even when a command or a restart has read a previous save.
// vanilla: LivingEntity, Rabbit, Goat, Dolphin.addAdditionalSaveData
func TestMobNativeFieldsKeepInsertionHistory(t *testing.T) {
	t.Parallel()
	cfg := servertest.BlockTestConfig()
	cfg.LevelName = t.TempDir()
	cfg.ViewDistance, cfg.SimulationDistance = config.MinDistance, config.MinDistance
	srv, c := servertest.Join(t, cfg, "Alice", servertest.OpsListing(t, "Alice"))
	servertest.HoldTicks(t, srv)
	fixtures := []struct {
		kind, first, second string
	}{
		{"rabbit", "DeathTime", "RabbitType"},
		{"goat", "DeathTime", "IsScreamingGoat"},
		{"dolphin", "NoAI", "GotFish"},
	}
	expect := func(t *testing.T, c *servertest.Client, kind, first, second string) {
		t.Helper()
		selector := "@e[type=" + kind + ",limit=1]"
		for _, cmd := range []string{
			"data get entity " + selector,
			`tellraw @s {nbt:"{}",entity:"` + selector + `"}`,
			`tellraw @s {nbt:"{}",entity:"` + selector + `",plain:1b}`,
		} {
			c.ChatCommand(cmd)
			line := servertest.FeedbackComponent(t, c)
			text := line.Plain()
			if strings.HasPrefix(cmd, "data ") {
				if line.Translate != "commands.data.entity.query" || len(line.With) != 2 {
					t.Fatalf("%s: feedback = %+v", cmd, line)
				}
				text = line.With[1].Plain()
			}
			keys := rootCompoundKeys(text)
			if i := slices.Index(keys, first); i < 0 || i+1 >= len(keys) || keys[i+1] != second {
				t.Errorf("%s: keys = %v, want adjacent %s, %s", cmd, keys, first, second)
			}
		}
	}
	for _, fixture := range fixtures {
		t.Run(fixture.kind, func(t *testing.T) {
			c.ChatCommand("summon " + fixture.kind + ` 4.5 70 4.5 {NoGravity:1b,NoAI:1b,Tags:["nativeorder"]}`)
			servertest.AwaitSystemChatSaying(t, c, "commands.summon.success")
			c.Settle()
			expect(t, c, fixture.kind, fixture.first, fixture.second)
			c.ChatCommand("data merge entity @e[type=" + fixture.kind + `,limit=1] {Health:2f}`)
			servertest.AwaitSystemChatSaying(t, c, "commands.data.entity.modified")
			c.Settle()
			expect(t, c, fixture.kind, fixture.first, fixture.second)
		})
	}
	srv.FlushMobs()
	c.Disconnect()
	srv.Shutdown()
	again, connect := servertest.Start(t, cfg, servertest.OpsListing(t, "Alice"))
	servertest.FreezeUntilHeld(t, again)
	client := connect().Join("Alice")
	servertest.HoldTicks(t, again)
	servertest.UntilTicks(t, again, 20, "saved mobs wake", func() bool {
		for _, fixture := range fixtures {
			if len(again.SelectedEntities("minecraft:"+fixture.kind)) != 1 {
				return false
			}
		}
		return true
	})
	client.Settle()
	for _, fixture := range fixtures {
		t.Run("restart/"+fixture.kind, func(t *testing.T) {
			expect(t, client, fixture.kind, fixture.first, fixture.second)
		})
	}
}
