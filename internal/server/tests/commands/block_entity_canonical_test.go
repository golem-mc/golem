package commands

import (
	"fmt"
	"strings"
	"testing"

	"github.com/golem-mc/golem/internal/config"
	"github.com/golem-mc/golem/internal/server"
	"github.com/golem-mc/golem/internal/server/servertest"
)

// vanilla: BlockEntity.saveWithFullMetadata, TextComponentTagVisitor
func TestCommandDataBlockEntityKeepsVanillaCodecForm(t *testing.T) {
	t.Parallel()
	cfg := servertest.BlockTestConfig()
	cfg.LevelName = t.TempDir()
	cfg.ViewDistance, cfg.SimulationDistance = config.MinDistance, config.MinDistance
	freeze := func(s *server.Server) { servertest.FreezeUntilHeld(t, s) }
	ops := servertest.OpsListing(t, "Setter")
	srv, c := servertest.Join(t, cfg, "Setter", ops, freeze)
	servertest.HoldTicks(t, srv)

	const pendingSpawn = `spawn_data: {entity: {id: "minecraft:zombie"}}, `
	rows := []struct{ typed, shown string }{
		{`white_banner{patterns:[{pattern:"creeper",color:"red"}],CustomName:"B"}`, `{components: {}, CustomName: "B", patterns: [{color: "red", pattern: "minecraft:creeper"}], x: @X, y: 63, z: 1, id: "minecraft:banner"}`},
		{`trial_spawner{normal_config:{spawn_potentials:[{data:{entity:{id:"zombie"}},weight:1}]}}`, `{components: {}, normal_config: {spawn_potentials: [{data: {entity: {id: "minecraft:zombie"}}, weight: 1}]}, x: @X, y: 63, @Dz: 1, id: "minecraft:trial_spawner"}`},
		{`vault{config:{key_item:{id:"trial_key",count:1}}}`, `{components: {}, server_data: {}, x: @X, y: 63, z: 1, id: "minecraft:vault", config: {key_item: {count: 1, id: "minecraft:trial_key"}}, shared_data: {}}`},
		{`player_head{profile:{name:"Notch"}}`, `{components: {}, profile: {name: "Notch"}, x: @X, y: 63, z: 1, id: "minecraft:skull"}`},
		{`player_head{profile:"Notch",custom_name:"x"}`, `{components: {}, custom_name: "x", profile: {name: "Notch"}, x: @X, y: 63, z: 1, id: "minecraft:skull"}`},
		{`chest{lock:{items:"diamond"}}`, `{components: {}, x: @X, lock: {items: "minecraft:diamond"}, y: 63, Items: [], z: 1, id: "minecraft:chest"}`},
		{`barrel{LootTable:"chests/simple_dungeon",LootTableSeed:5L}`, `{LootTable: "minecraft:chests/simple_dungeon", components: {}, x: @X, y: 63, z: 1, id: "minecraft:barrel", LootTableSeed: 5L}`},
		{`end_gateway{Age:5L,ExactTeleport:1b,exit_portal:[I;1,2,3]}`, `{components: {}, ExactTeleport: 1b, x: @X, y: 63, z: 1, id: "minecraft:end_gateway", exit_portal: [I; 1, 2, 3], Age: 5L}`},
		{`test_block{mode:"log",message:"hi"}`, `{mode: "log", components: {}, powered: 0b, x: @X, y: 63, z: 1, id: "minecraft:test_block", message: "hi"}`},
		{`structure_block{mode:"SAVE",name:"x:y",sizeX:3,sizeY:3,sizeZ:3}`, `{metadata: "", mirror: "NONE", components: {}, ignoreEntities: 1b, powered: 0b, seed: 0L, author: "", rotation: "NONE", posX: 0, mode: "SAVE", posY: 1, sizeX: 3, posZ: 0, integrity: 1.0f, showair: 0b, name: "x:y", x: @X, y: 63, z: 1, id: "minecraft:structure_block", strict: 0b, sizeY: 3, sizeZ: 3, showboundingbox: 1b}`},
		{`suspicious_sand{item:{id:"diamond",count:1}}`, `{item: {count: 1, id: "minecraft:diamond"}, components: {}, x: @X, y: 63, z: 1, id: "minecraft:brushable_block"}`},
		{`test_instance_block{data:{test:"x:y",size:[I;1,2,3],rotation:"180",ignore_entities:1b,status:"cleared"}}`, `{components: {}, data: {test: "x:y", size: [I; 1, 2, 3], ignore_entities: 1b, rotation: "180", status: "cleared"}, x: @X, y: 63, z: 1, id: "minecraft:test_instance_block"}`},
		{`test_instance_block{data:{test:"x:y"}}`, `{components: {}, data: {size: [I; 0, 0, 0], ignore_entities: 0b, rotation: "none", status: "cleared"}, x: @X, y: 63, z: 1, id: "minecraft:test_instance_block"}`},
		{`test_instance_block{data:{status:"running",ignore_entities:2s,rotation:"clockwise_90",size:[1,2,3],test:"always_pass",error_message:{text:"Oops"},extra:1}}`, `{components: {}, data: {error_message: "Oops", test: "minecraft:always_pass", size: [I; 1, 2, 3], ignore_entities: 1b, rotation: "clockwise_90", status: "running"}, x: @X, y: 63, z: 1, id: "minecraft:test_instance_block"}`},
		{`trial_spawner{normal_config:{items_to_drop_when_ominous:"foo",loot_tables_to_eject:[{weight:1,data:"bar"}],spawn_potentials:[{weight:1,data:{entity:{id:"zombie"}}}],ticks_between_spawn:20,simultaneous_mobs_added_per_player:3f,total_mobs_added_per_player:3f,simultaneous_mobs:3f,total_mobs:3f,spawn_range:3}}`, `{components: {}, normal_config: {ticks_between_spawn: 20, items_to_drop_when_ominous: "minecraft:foo", spawn_potentials: [{data: {entity: {id: "minecraft:zombie"}}, weight: 1}], loot_tables_to_eject: [{data: "minecraft:bar", weight: 1}], simultaneous_mobs: 3.0f, simultaneous_mobs_added_per_player: 3.0f, spawn_range: 3, total_mobs_added_per_player: 3.0f, total_mobs: 3.0f}, x: @X, y: 63, @Dz: 1, id: "minecraft:trial_spawner"}`},
		{`vault{config:{override_loot_table_to_display:"foo",key_item:{id:"trial_key"},deactivation_range:9,activation_range:8,loot_table:"bar"}}`, `{components: {}, server_data: {}, x: @X, y: 63, z: 1, id: "minecraft:vault", config: {deactivation_range: 9.0d, override_loot_table_to_display: "minecraft:foo", key_item: {count: 1, id: "minecraft:trial_key"}, activation_range: 8.0d, loot_table: "minecraft:bar"}, shared_data: {}}`},
		{`chiseled_bookshelf{components:{"minecraft:custom_data":{foo:1}},last_interacted_slot:3}`, `{last_interacted_slot: 3, components: {"minecraft:custom_data": {foo: 1}}, x: @X, y: 63, Items: [], z: 1, id: "minecraft:chiseled_bookshelf"}`},
		{`chest{lock:{items:["diamond"]}}`, `{components: {}, x: @X, lock: {items: "minecraft:diamond"}, y: 63, Items: [], z: 1, id: "minecraft:chest"}`},
		{`test_block{mode:"bogus",message:3,powered:1}`, `{mode: "fail", components: {}, powered: 1b, x: @X, y: 63, z: 1, id: "minecraft:test_block", message: ""}`},
		{`structure_block{mode:"SAVE",ignoreEntities:0,powered:1,integrity:0.5d,posX:90,sizeX:71}`, `{metadata: "", mirror: "NONE", components: {}, ignoreEntities: 0b, powered: 1b, seed: 0L, author: "", rotation: "NONE", posX: 48, mode: "SAVE", posY: 1, sizeX: 48, posZ: 0, integrity: 0.5f, showair: 0b, name: "", x: @X, y: 63, z: 1, id: "minecraft:structure_block", strict: 0b, sizeY: 0, sizeZ: 0, showboundingbox: 1b}`},
	}
	expectShown := func(srv *server.Server, when, spawnData string) {
		t.Helper()
		for i, row := range rows {
			if when != "placed" && (strings.HasPrefix(row.typed, "end_gateway") || strings.HasPrefix(row.typed, "vault")) {
				continue // Age and vault timers depend on the tick.
			}
			x := fmt.Sprint(2 * i)
			want := strings.NewReplacer("@X", x, "@D", spawnData).Replace(row.shown)
			if got := plainQuery(t, srv, "data get block "+x+" 63 1", "commands.data.block.query"); got != want {
				t.Errorf("%s, %s at x %s = %s, want %s", when, row.typed, x, got, want)
			}
		}
	}
	for i, row := range rows {
		x := fmt.Sprint(2 * i)
		c.ChatCommand("setblock " + x + " 63 1 minecraft:" + row.typed)
		c.ExpectCommandFeedback("commands.setblock.success", x, "63", "1")
	}
	expectShown(srv, "placed", "")

	srv.RunLine(`data modify block 22 63 1 data set value {test:"ignored"}`)
	if got, want := plainQuery(t, srv, "data get block 22 63 1", "commands.data.block.query"), strings.ReplaceAll(rows[11].shown, "@X", "22"); got != want {
		t.Errorf("test instance block after an invalid edit = %s, want %s", got, want)
	}

	servertest.RunTicks(t, srv, 1)
	expectShown(srv, "after a tick", pendingSpawn)

	c.Conn.Close()
	srv.Shutdown()
	restarted := *cfg
	back, _ := servertest.Join(t, &restarted, "Setter", ops, freeze)
	servertest.HoldTicks(t, back)
	expectShown(back, "after a restart", pendingSpawn)
}

// vanilla: MerchantOffers.CODEC, MerchantOffer.CODEC
func TestCommandDataVillagerOffersKeepVanillaCodecForm(t *testing.T) {
	t.Parallel()
	cfg := servertest.BlockTestConfig()
	cfg.ViewDistance, cfg.SimulationDistance = config.MinDistance, config.MinDistance
	srv, _ := servertest.Join(t, cfg, "Trader", servertest.OpsListing(t, "Trader"))
	servertest.HoldTicks(t, srv)

	villager := "@e[type=villager,limit=1]"
	srv.RunLine(`summon villager 1 63 1 {Offers:{Recipes:[{buy:{id:"emerald",count:1},sell:{id:"bread",count:2},maxUses:5}]}}`)
	query := "data get entity " + villager + " Offers"
	if got, want := plainQuery(t, srv, query, "commands.data.entity.query"), `{Recipes: [{maxUses: 5, buy: {count: 1, id: "minecraft:emerald"}, sell: {count: 2, id: "minecraft:bread"}}]}`; got != want {
		t.Errorf("summoned villager Offers = %s, want %s", got, want)
	}
	srv.RunLine("data modify entity " + villager + ` Offers set value {Recipes:[{buy:{id:"emerald"},sell:{id:"bread"},buyB:{id:"missing"},uses:5b,maxUses:4s,rewardExp:0s,specialPrice:-2L,demand:"wrong",priceMultiplier:0.5d,xp:1b,unknown:1},{buy:{id:"missing"},sell:{id:"bread"}},{buy:{id:"emerald",components:{custom_name:{text:"coin"}}},buyB:{id:"stick",count:0},sell:{id:"bread",count:"wrong",components:{custom_name:{text:"loaf"},unknown:1}}}],unknown:2}`)
	if got, want := plainQuery(t, srv, query, "commands.data.entity.query"), `{Recipes: [{buy: {count: 1, id: "minecraft:emerald"}, sell: {count: 1, id: "minecraft:bread"}, uses: 5, specialPrice: -2, priceMultiplier: 0.5f, rewardExp: 0b}, {buy: {components: {"minecraft:custom_name": "coin"}, count: 1, id: "minecraft:emerald"}, sell: {components: {"minecraft:custom_name": "loaf"}, count: 1, id: "minecraft:bread"}}]}`; got != want {
		t.Errorf("edited villager Offers = %s, want %s", got, want)
	}
	srv.RunLine("data modify entity " + villager + ` Offers.Recipes set value []`)
	if got := plainQuery(t, srv, query, "commands.data.entity.query"); got != "{}" {
		t.Errorf("empty villager Offers = %s, want {}", got)
	}
}

// plainQuery returns the SNBT shown in a data query's feedback.
func plainQuery(t *testing.T, srv *server.Server, line, key string) string {
	t.Helper()
	replies := srv.RunLine(line)
	for _, text := range replies {
		if text.Translate == key && len(text.With) > 0 {
			return text.With[len(text.With)-1].Plain()
		}
	}
	t.Fatalf("%s answered %+v", line, replies)
	return ""
}
