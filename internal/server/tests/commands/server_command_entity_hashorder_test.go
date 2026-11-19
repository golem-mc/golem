package commands

import (
	"regexp"
	"slices"
	"strings"
	"testing"

	"github.com/golem-mc/golem/internal/config"
	"github.com/golem-mc/golem/internal/server/servertest"
)

func compoundOrderWorld(t *testing.T) *servertest.Client {
	t.Helper()
	cfg := config.Default()
	cfg.OnlineMode = false
	cfg.ViewDistance, cfg.SimulationDistance = config.MinDistance, config.MinDistance
	srv, c := servertest.Join(t, cfg, "Alice", servertest.OpsListing(t, "Alice"))
	servertest.HoldTicks(t, srv)
	return c
}

// vanilla: Entity.saveWithoutId, LivingEntity.addAdditionalSaveData,
// ArmorStand.addAdditionalSaveData, TextComponentTagVisitor.visitCompound
func TestArmorStandCompoundInsertionHistory(t *testing.T) {
	t.Parallel()
	for _, fixture := range []struct{ tag, keys string }{
		{`{}`, "Motion Brain Small Health Invulnerable FallFlying fall_distance Air OnGround ShowArms PortalCooldown AbsorptionAmount Rotation current_impulse_context_reset_grace_time DisabledSlots Pos DeathTime Fire Pose NoBasePlate attributes Invisible UUID HurtTime"},
		{`{NoGravity:1b,Tags:["order"]}`, "NoGravity Brain Invulnerable FallFlying ShowArms PortalCooldown AbsorptionAmount DisabledSlots DeathTime Pose Invisible UUID Tags Motion Small Health fall_distance Air OnGround Rotation current_impulse_context_reset_grace_time Pos Fire NoBasePlate attributes HurtTime"},
	} {
		t.Run(fixture.tag, func(t *testing.T) {
			c := compoundOrderWorld(t)
			c.ChatCommand(`summon armor_stand ~ ~ ~ ` + fixture.tag)
			servertest.AwaitSystemChatSaying(t, c, "commands.summon.success")
			c.Settle()
			for _, change := range []string{"", `data merge entity @e[type=armor_stand,limit=1] {Small:1b,AbsorptionAmount:3.0f}`} {
				if change != "" {
					c.ChatCommand(change)
					servertest.AwaitSystemChatSaying(t, c, "commands.data.entity.modified")
					c.Settle()
				}
				for _, cmd := range []string{
					`data get entity @e[type=armor_stand,limit=1]`,
					`tellraw @s {nbt:"{}",entity:"@e[type=armor_stand,limit=1]"}`,
					`tellraw @s {nbt:"{}",entity:"@e[type=armor_stand,limit=1]",plain:1b}`,
				} {
					c.ChatCommand(cmd)
					line := servertest.FeedbackComponent(t, c)
					text := line.Plain()
					if strings.HasPrefix(cmd, "data ") {
						text = line.With[1].Plain()
					}
					if change != "" && !strings.Contains(text, "AbsorptionAmount: 3.0f") {
						t.Errorf("%s: absorption write missing from %s", cmd, text)
					}
					var keys []string
					for _, match := range regexp.MustCompile(`([A-Za-z_]+):`).FindAllStringSubmatch(text, -1) {
						if match[1] != "memories" {
							keys = append(keys, match[1])
						}
					}
					if !slices.Equal(keys, strings.Fields(fixture.keys)) {
						t.Errorf("%s: keys = %v, want %s", cmd, keys, fixture.keys)
					}

					for _, keys := range [][]string{
						{"Invulnerable", "FallFlying"},
						{"PortalCooldown", "AbsorptionAmount"},
						{"fall_distance", "Air", "OnGround"},
					} {
						last := -1
						for _, key := range keys {
							at := strings.Index(text, key+":")
							if at <= last {
								t.Errorf("%s: key %s out of order in %s", cmd, key, text)
							}
							last = at
						}
					}
				}
			}
		})
	}
}

// vanilla: AbstractFurnaceBlockEntity.saveAdditional, BaseSpawner.save,
// BlockEntity.saveWithFullMetadata, TextComponentTagVisitor.visitCompound
func TestBlockEntityCompoundInsertionHistory(t *testing.T) {
	t.Parallel()
	c := compoundOrderWorld(t)
	for _, fixture := range []struct{ block, want string }{
		{`furnace{cooking_total_time:200}`, `{lit_total_time: 0, components: {}, cooking_time_spent: 0, speed_multiplier: 1.0f, x: 2, y: 64, cooking_total_time: 200, Items: [], z: 3, id: "minecraft:furnace", lit_time_remaining: 0, RecipesUsed: {}}`},
		{`spawner{SpawnData:{entity:{id:"minecraft:pig"}}}`, `{components: {}, MaxNearbyEntities: 6s, RequiredPlayerRange: 16s, SpawnCount: 4s, SpawnData: {entity: {id: "minecraft:pig"}}, MaxSpawnDelay: 800s, Delay: 20s, x: 2, y: 64, z: 3, id: "minecraft:mob_spawner", SpawnRange: 4s, MinSpawnDelay: 200s, SpawnPotentials: [{data: {entity: {id: "minecraft:pig"}}, weight: 1}]}`},
	} {
		c.ChatCommand(`setblock 2 64 3 ` + fixture.block)
		servertest.AwaitSystemChatSaying(t, c, "commands.setblock.success")
		c.Settle()
		// Re-loading a /data write must rebuild the same compound through the codec.
		c.ChatCommand(`data merge block 2 64 3 {x:99}`)
		c.ExpectCommandFeedback("commands.data.block.modified", "2", "64", "3")
		for _, cmd := range []string{
			`data get block 2 64 3`,
			`tellraw @s {nbt:"{}",block:"2 64 3"}`,
			`tellraw @s {nbt:"{}",block:"2 64 3",plain:1b}`,
		} {
			c.ChatCommand(cmd)
			line := servertest.FeedbackComponent(t, c)
			text := line.Plain()
			if strings.HasPrefix(cmd, "data ") {
				text = line.With[3].Plain()
			}
			if text != fixture.want {
				t.Errorf("%s, %s: got %s, want %s", fixture.block, cmd, text, fixture.want)
			}
		}
	}
}

// vanilla: StringTagVisitor.visitCompound
func TestBlockEntityStringNbtRemainsSorted(t *testing.T) {
	t.Parallel()
	c := compoundOrderWorld(t)
	c.ChatCommand(`setblock 2 64 3 furnace{cooking_total_time:200}`)
	servertest.AwaitSystemChatSaying(t, c, "commands.setblock.success")
	c.Settle()
	c.ChatCommand(`data modify storage order text set string block 2 64 3`)
	const want = `{Items:[],RecipesUsed:{},components:{},cooking_time_spent:0,cooking_total_time:200,id:"minecraft:furnace",lit_time_remaining:0,lit_total_time:0,speed_multiplier:1.0f,x:2,y:64,z:3}`
	servertest.AwaitCommandError(t, c, "commands.data.modify.expected_value", want)
}
