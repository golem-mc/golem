package commands

import (
	"slices"
	"strings"
	"testing"

	"github.com/golem-mc/golem/internal/config"
	"github.com/golem-mc/golem/internal/server/servertest"
	"github.com/golem-mc/golem/nbt"
	"github.com/golem-mc/golem/registry"
)

// Recorded Java HashMap iteration for the supported fields, inserted in the order
// of Entity.saveWithoutId and each kind's addAdditionalSaveData in vanilla 26.3.
var nonlivingCompoundHistory = []struct{ kind, tag, keys string }{
	{"item", `{NoGravity:1b,Item:{id:"minecraft:stone",count:1},PickupDelay:32767s,Owner:[I;1,2,3,4],Tags:["order"]}`, "NoGravity Motion Owner Health Invulnerable fall_distance Air OnGround PortalCooldown Rotation Item Pos PickupDelay Fire UUID Tags Age"},
	{"experience_orb", `{NoGravity:1b,Tags:["order"]}`, "NoGravity Motion Health Invulnerable fall_distance Air OnGround Count PortalCooldown Rotation Pos Fire Value UUID Tags Age"},
	{"tnt", `{NoGravity:1b,fuse:300s,explosion_power:8f,owner:[I;1,2,3,4],Tags:["order"]}`, "NoGravity owner Motion Invulnerable fall_distance Air OnGround PortalCooldown Rotation fuse explosion_power Pos block_state Fire UUID Tags"},
	{"falling_block", `{NoGravity:1b,BlockState:{Name:"minecraft:stone"},Tags:["order"]}`, "NoGravity Motion FallHurtMax Invulnerable Time fall_distance Air OnGround PortalCooldown Rotation DropItem HurtEntities BlockState Pos CancelDrop Fire UUID FallHurtAmount Tags"},
	{"snowball", `{NoGravity:1b,Owner:[I;1,2,3,4],LeftOwner:1b,HasBeenShot:1b,Tags:["order"]}`, "NoGravity Motion Owner Invulnerable LeftOwner fall_distance Air OnGround PortalCooldown Rotation Item Pos HasBeenShot Fire UUID Tags"},
	{"arrow", `{NoGravity:1b,Owner:[I;1,2,3,4],HasBeenShot:1b,inBlockState:{Name:"minecraft:stone"},Tags:["order"]}`, "NoGravity Motion damage Owner PierceLevel item shake inGround pickup Invulnerable fall_distance Air OnGround PortalCooldown Rotation life inBlockState SoundEvent crit Pos HasBeenShot Fire UUID Tags"},
	{"spectral_arrow", `{NoGravity:1b,Owner:[I;1,2,3,4],HasBeenShot:1b,inBlockState:{Name:"minecraft:stone"},Tags:["order"]}`, "NoGravity damage Owner shake Invulnerable PortalCooldown life SoundEvent UUID Tags Motion PierceLevel item inGround pickup Duration fall_distance Air OnGround Rotation inBlockState crit Pos HasBeenShot Fire"},
	{"trident", `{NoGravity:1b,Owner:[I;1,2,3,4],HasBeenShot:1b,DealtDamage:1b,inBlockState:{Name:"minecraft:stone"},Tags:["order"]}`, "NoGravity damage Owner shake Invulnerable PortalCooldown life SoundEvent UUID Tags Motion PierceLevel item inGround pickup fall_distance Air OnGround Rotation inBlockState crit Pos HasBeenShot Fire DealtDamage"},
	{"fireball", `{NoGravity:1b,Owner:[I;1,2,3,4],LeftOwner:1b,HasBeenShot:1b,Tags:["order"]}`, "NoGravity Motion Owner Invulnerable LeftOwner fall_distance Air OnGround ExplosionPower PortalCooldown Rotation Item Pos HasBeenShot Fire UUID acceleration_power Tags"},
	{"shulker_bullet", `{NoGravity:1b,Owner:[I;1,2,3,4],LeftOwner:1b,HasBeenShot:1b,Target:[I;1,2,3,4],Dir:1b,Tags:["order"]}`, "NoGravity Steps Motion TXD Owner Invulnerable LeftOwner fall_distance Air OnGround PortalCooldown Dir Rotation Target Pos HasBeenShot Fire UUID Tags TYD TZD"},
	{"area_effect_cloud", `{Owner:[I;1,2,3,4],potion_contents:{potion:"minecraft:water"},potion_duration_scale:2f,Tags:["order"]}`, "Motion ReapplicationDelay Radius Owner DurationOnUse Invulnerable Duration fall_distance Air OnGround PortalCooldown Rotation RadiusPerTick Pos Fire potion_contents RadiusOnUse potion_duration_scale WaitTime UUID Tags Age"},
	{"firework_rocket", `{Owner:[I;1,2,3,4],LeftOwner:1b,HasBeenShot:1b,LifeTime:300,Tags:["order"]}`, "Motion Owner LifeTime Invulnerable LeftOwner fall_distance Air OnGround PortalCooldown Rotation Pos HasBeenShot Fire FireworksItem Life ShotAtAngle UUID Tags"},
	{"oak_boat", `{NoGravity:1b,Tags:["order"]}`, "NoGravity Motion Pos Fire Invulnerable fall_distance Air OnGround PortalCooldown UUID Rotation Tags"},
	{"chest_minecart", `{NoGravity:1b,DisplayState:{Name:"minecraft:stone"},DisplayOffset:9,Tags:["order"]}`, "NoGravity Motion DisplayState Invulnerable fall_distance Air OnGround PortalCooldown Rotation DisplayOffset Pos Fire Items UUID FlippedRotation HasTicked Tags"},
	{"minecart", `{NoGravity:1b,Tags:["order"]}`, "NoGravity Motion Invulnerable fall_distance Air OnGround PortalCooldown Rotation Pos Fire UUID FlippedRotation HasTicked Tags"},
	{"furnace_minecart", `{NoGravity:1b,Tags:["order"]}`, "NoGravity PushX Motion Invulnerable fall_distance Air OnGround PortalCooldown Rotation Pos Fire Fuel UUID FlippedRotation HasTicked Tags PushZ"},
	{"hopper_minecart", `{NoGravity:1b,Tags:["order"]}`, "NoGravity Motion Enabled Invulnerable fall_distance Air OnGround PortalCooldown Rotation Pos Fire Items UUID FlippedRotation HasTicked Tags"},
	{"tnt_minecart", `{NoGravity:1b,fuse:300,explosion_power:8f,explosion_speed_factor:2f,Tags:["order"]}`, "NoGravity Motion Invulnerable fall_distance Air OnGround PortalCooldown Rotation fuse explosion_power Pos Fire explosion_speed_factor UUID FlippedRotation HasTicked Tags"},
	{"spawner_minecart", `{NoGravity:1b,Delay:300s,SpawnData:{entity:{id:"minecraft:pig"}},Tags:["order"]}`, "NoGravity Motion MaxNearbyEntities RequiredPlayerRange SpawnCount Invulnerable SpawnData fall_distance Air OnGround MaxSpawnDelay PortalCooldown Rotation Delay Pos Fire UUID FlippedRotation SpawnRange HasTicked Tags MinSpawnDelay SpawnPotentials"},
	{"command_block_minecart", `{NoGravity:1b,Tags:["order"]}`, "NoGravity Motion Invulnerable fall_distance Air OnGround PortalCooldown SuccessCount Rotation UpdateLastExecution Pos Command Fire UUID FlippedRotation HasTicked Tags TrackOutput"},
	{"oak_chest_boat", `{NoGravity:1b,leash:{UUID:[I;1,2,3,4]},Tags:["order"]}`, "NoGravity Motion Invulnerable fall_distance Air OnGround PortalCooldown Rotation Pos Fire Items UUID leash Tags"},
	{"painting", `{variant:"minecraft:kebab",Tags:["order"]}`, "Motion block_pos facing Invulnerable fall_distance Air OnGround PortalCooldown Rotation Pos Fire variant UUID Tags"},
	{"item_frame", `{Item:{id:"minecraft:stone",count:1},Tags:["order"]}`, "Motion Facing block_pos ItemRotation Invulnerable fall_distance Air OnGround PortalCooldown Rotation Item ItemDropChance Pos Fire Invisible UUID Tags Fixed"},
	{"end_crystal", `{beam_target:[I;1,70,2],Tags:["order"]}`, "Motion beam_target Invulnerable fall_distance Air OnGround PortalCooldown Rotation Pos Fire UUID Tags ShowBottom"},
	{"eye_of_ender", `{NoGravity:1b,Tags:["order"]}`, "NoGravity Motion Invulnerable fall_distance Air OnGround PortalCooldown Rotation Item Pos Fire UUID Tags"},
	{"leash_knot", `{}`, "Motion Pos Fire Invulnerable fall_distance Air OnGround PortalCooldown UUID Rotation"},
	{"evoker_fangs", `{NoGravity:1b,Owner:[I;1,2,3,4],Warmup:300,Tags:["order"]}`, "NoGravity Motion Warmup Owner Invulnerable fall_distance Air OnGround PortalCooldown Rotation Pos Fire UUID Tags"},
	{"lightning_bolt", `{NoGravity:1b,Tags:["order"]}`, "NoGravity Motion Pos Fire Invulnerable fall_distance Air OnGround PortalCooldown UUID Rotation Tags"},
}

// rootCompoundKeys reads the emitted order without rehashing or sorting it.
func rootCompoundKeys(text string) []string {
	var keys []string
	depth, start := 0, 0
	quoted, escaped := false, false
	for i, ch := range text {
		if quoted {
			if escaped {
				escaped = false
			} else if ch == '\\' {
				escaped = true
			} else if ch == '"' {
				quoted = false
			}
			continue
		}
		switch ch {
		case '"':
			quoted = true
		case '{', '[':
			depth++
			if depth == 1 {
				start = i + 1
			}
		case '}', ']':
			depth--
		case ',':
			if depth == 1 {
				start = i + 1
			}
		case ':':
			if depth == 1 {
				keys = append(keys, strings.TrimSpace(text[start:i]))
			}
		}
	}
	return keys
}

func expectNonlivingHistory(t *testing.T, c *servertest.Client, kind, keys string) []string {
	t.Helper()
	selector := "@e[type=minecraft:" + kind + ",limit=1]"
	var texts []string
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
		if got := rootCompoundKeys(text); !slices.Equal(got, strings.Fields(keys)) {
			t.Errorf("%s: keys = %s, want %s", cmd, strings.Join(got, " "), keys)
		}
		texts = append(texts, text)
	}
	return texts
}

func expectNonlivingTagOrder(t *testing.T, c *servertest.Client, kind, want string) {
	t.Helper()
	c.ChatCommand("data get entity @e[type=minecraft:" + kind + ",limit=1] Tags")
	line := servertest.FeedbackComponent(t, c)
	if line.Translate != "commands.data.entity.query" || len(line.With) != 2 || line.With[1].Plain() != `["`+want+`", "order"]` {
		t.Fatalf("%s: tag order = %+v, want %s before order", kind, line, want)
	}
}

// vanilla: AreaEffectCloud.addAdditionalSaveData
func TestAreaEffectCloudDurationScaleSave(t *testing.T) {
	t.Parallel()
	const scaledKeys = "Motion ReapplicationDelay Radius DurationOnUse Invulnerable Duration fall_distance Air OnGround PortalCooldown Rotation RadiusPerTick Pos Fire potion_contents RadiusOnUse potion_duration_scale WaitTime UUID Age"
	for _, tc := range []struct{ name, tag string }{
		{"omitted", `{}`},
		{"explicit_default", `{potion_duration_scale:1f}`},
		{"nondefault", `{potion_contents:{potion:"minecraft:water"},potion_duration_scale:2f}`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			cfg := servertest.BlockTestConfig()
			cfg.ViewDistance = config.MinDistance
			cfg.SimulationDistance = config.MinDistance
			srv, c := servertest.Join(t, cfg, "Alice", servertest.OpsListing(t, "Alice"))
			servertest.HoldTicks(t, srv)
			c.ChatCommand("summon area_effect_cloud 4.5 70 4.5 " + tc.tag)
			servertest.AwaitSystemChatSaying(t, c, "commands.summon.success")
			c.Settle()
			const selector = "@e[type=minecraft:area_effect_cloud,limit=1]"
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
				tag, err := nbt.ParseSNBT(text)
				if err != nil {
					t.Fatalf("%s: parse compound: %v", cmd, err)
				}
				scale, present := tag["potion_duration_scale"]
				if tc.name != "nondefault" {
					if present {
						t.Errorf("%s: default scale was saved: %v", cmd, scale)
					}
					continue
				}
				if scale != nbt.Float(2) {
					t.Errorf("%s: scale = %v, want 2.0f", cmd, scale)
				}
				if got := rootCompoundKeys(text); !slices.Equal(got, strings.Fields(scaledKeys)) {
					t.Errorf("%s: keys = %s, want %s", cmd, strings.Join(got, " "), scaledKeys)
				}
			}
		})
	}
}

// vanilla: AreaEffectCloud.addAdditionalSaveData, PotionContents.EMPTY
func TestAreaEffectCloudPotionContentsSave(t *testing.T) {
	t.Parallel()
	const emptyKeys = "Motion ReapplicationDelay Radius DurationOnUse Invulnerable Duration fall_distance Air OnGround PortalCooldown Rotation RadiusPerTick Pos Fire RadiusOnUse WaitTime UUID Age"
	const contentsKeys = "Motion ReapplicationDelay Radius DurationOnUse Invulnerable Duration fall_distance Air OnGround PortalCooldown Rotation RadiusPerTick Pos Fire potion_contents RadiusOnUse WaitTime UUID Age"
	for _, tc := range []struct{ name, tag, want string }{
		{"omitted", `{}`, `{}`},
		{"explicit_empty", `{potion_contents:{}}`, `{}`},
		{"empty_effects", `{potion_contents:{custom_effects:[]}}`, `{}`},
		{"potion_without_effects", `{potion_contents:{potion:"minecraft:water"}}`, `{potion_contents:{potion:"minecraft:water"}}`},
		{"color_only", `{potion_contents:{custom_color:0}}`, `{potion_contents:{custom_color:0}}`},
		{"empty_name", `{potion_contents:{custom_name:""}}`, `{potion_contents:{custom_name:""}}`},
		{"effect_only", `{potion_contents:{custom_effects:[{id:"minecraft:poison",duration:100}]}}`, `{potion_contents:{custom_effects:[{id:"minecraft:poison",duration:100,show_icon:1b}]}}`},
		{"all_fields_empty_effects", `{potion_contents:{potion:"minecraft:water",custom_color:0,custom_effects:[],custom_name:""}}`, `{potion_contents:{potion:"minecraft:water",custom_color:0,custom_name:""}}`},
		{"multiple_fields", `{potion_contents:{potion:"minecraft:water",custom_color:0,custom_name:""}}`, `{potion_contents:{potion:"minecraft:water",custom_color:0,custom_name:""}}`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			cfg := servertest.BlockTestConfig()
			cfg.ViewDistance = config.MinDistance
			cfg.SimulationDistance = config.MinDistance
			srv, c := servertest.Join(t, cfg, "Alice", servertest.OpsListing(t, "Alice"))
			servertest.HoldTicks(t, srv)
			c.ChatCommand("summon area_effect_cloud 4.5 70 4.5 " + tc.tag)
			servertest.AwaitSystemChatSaying(t, c, "commands.summon.success")
			c.Settle()
			want, err := nbt.ParseSNBT(tc.want)
			if err != nil {
				t.Fatal(err)
			}
			keys := emptyKeys
			if want["potion_contents"] != nil {
				keys = contentsKeys
			}
			for _, text := range expectNonlivingHistory(t, c, "area_effect_cloud", keys) {
				tag, err := nbt.ParseSNBT(text)
				if err != nil {
					t.Fatal(err)
				}
				if !nbt.Equal(tag["potion_contents"], want["potion_contents"]) {
					t.Errorf("potion_contents = %v, want %v", tag["potion_contents"], want["potion_contents"])
				}
			}
		})
	}
}

func TestNonlivingCompoundInsertionHistory(t *testing.T) {
	t.Parallel()
	cfg := servertest.BlockTestConfig()
	cfg.ViewDistance = config.MinDistance
	cfg.SimulationDistance = config.MinDistance
	srv, c := servertest.Join(t, cfg, "Alice", servertest.OpsListing(t, "Alice"))
	servertest.HoldTicks(t, srv)
	for _, fixture := range nonlivingCompoundHistory {
		t.Run(fixture.kind, func(t *testing.T) {
			if fixture.kind == "leash_knot" {
				srv.SetBlock(4, 70, 4, registry.BlockOakFence.DefaultState())
			}
			c.ChatCommand("summon " + fixture.kind + " 4.5 70 4.5 " + fixture.tag)
			servertest.AwaitSystemChatSaying(t, c, "commands.summon.success")
			c.Settle()
			expectNonlivingHistory(t, c, fixture.kind, fixture.keys)
			if fixture.kind == "item" {
				c.ChatCommand(`data modify storage order text set string entity @e[type=item,limit=1]`)
				line := servertest.FeedbackComponent(t, c)
				if len(line.Extra) == 1 {
					line = line.Extra[0]
				}
				if line.Translate != "commands.data.modify.expected_value" || len(line.With) != 1 {
					t.Fatalf("compact SNBT feedback = %+v", line)
				}
				keys := rootCompoundKeys(line.With[0].Plain())
				if !slices.IsSorted(keys) || len(keys) != len(strings.Fields(fixture.keys)) {
					t.Errorf("compact SNBT keys = %v", keys)
				}
			}

			if slices.Contains([]string{"area_effect_cloud", "firework_rocket", "painting", "leash_knot"}, fixture.kind) {
				return
			}
			c.ChatCommand("data modify entity @e[type=minecraft:" + fixture.kind + ",limit=1] Tags append value written")
			servertest.AwaitSystemChatSaying(t, c, "commands.data.entity.modified")
			c.Settle()
			expectNonlivingHistory(t, c, fixture.kind, fixture.keys)
			expectNonlivingTagOrder(t, c, fixture.kind, "written")
		})
	}
}

func TestNonlivingCompoundHistoryPersists(t *testing.T) {
	t.Parallel()
	cfg := servertest.BlockTestConfig()
	cfg.LevelName = t.TempDir()
	cfg.ViewDistance = config.MinDistance
	cfg.SimulationDistance = config.MinDistance
	srv, c := servertest.Join(t, cfg, "Alice", servertest.OpsListing(t, "Alice"))
	servertest.HoldTicks(t, srv)
	persistent := []string{"item", "experience_orb", "tnt", "falling_block", "oak_boat", "chest_minecart"}
	for _, fixture := range nonlivingCompoundHistory {
		if !slices.Contains(persistent, fixture.kind) {
			continue
		}
		c.ChatCommand("summon " + fixture.kind + " 4.5 70 4.5 " + fixture.tag)
		servertest.AwaitSystemChatSaying(t, c, "commands.summon.success")
		c.Settle()
		c.ChatCommand("data modify entity @e[type=minecraft:" + fixture.kind + ",limit=1] Tags append value persisted")
		servertest.AwaitSystemChatSaying(t, c, "commands.data.entity.modified")
		c.Settle()
	}
	srv.FlushMobs()
	c.Disconnect()
	srv.Shutdown()
	again, connect := servertest.Start(t, cfg, servertest.OpsListing(t, "Alice"))
	servertest.FreezeUntilHeld(t, again)
	client := connect().Join("Alice")
	servertest.HoldTicks(t, again)
	servertest.UntilTicks(t, again, 20, "saved entities wake", func() bool {
		for _, kind := range persistent {
			if len(again.SelectedEntities("minecraft:"+kind)) != 1 {
				return false
			}
		}
		return true
	})
	for _, fixture := range nonlivingCompoundHistory {
		if slices.Contains(persistent, fixture.kind) {
			expectNonlivingHistory(t, client, fixture.kind, fixture.keys)
			expectNonlivingTagOrder(t, client, fixture.kind, "persisted")
		}
	}
}
