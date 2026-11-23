package commands

import (
	"strconv"
	"strings"
	"testing"

	"github.com/golem-mc/golem/internal/config"
	"github.com/golem-mc/golem/internal/server"
	"github.com/golem-mc/golem/internal/server/servertest"
)

// TestCommandDataBlockEntityKeepsVanillaCodecForm checks that a block entity
// a command creates answers /data get block with what vanilla's codecs write
// back: ids namespaced, components and each kind's always-written fields
// present.
// vanilla: BlockEntity.loadWithComponents, BlockEntity.saveWithFullMetadata
func TestCommandDataBlockEntityKeepsVanillaCodecForm(t *testing.T) {
	t.Parallel()
	cfg := servertest.BlockTestConfig()
	cfg.LevelName = t.TempDir()
	cfg.ViewDistance, cfg.SimulationDistance = config.MinDistance, config.MinDistance
	// Held, so no ticker (gateway age, vault data, trial spawner) rewrites
	// the entity between /setblock and /data get.
	freeze := func(s *server.Server) { servertest.FreezeUntilHeld(t, s) }
	srv, c := servertest.Join(t, cfg, "Setter", servertest.OpsListing(t, "Setter"), freeze)
	servertest.HoldTicks(t, srv)

	for i, row := range []struct{ typed, want string }{
		{`white_banner{patterns:[{pattern:"creeper",color:"red"}],CustomName:"B"}`, `{CustomName:"B",components:{},id:"minecraft:banner",patterns:[{color:"red",pattern:"minecraft:creeper"}],x:@X,y:63,z:1}`},
		{`trial_spawner{target_cooldown_length:600,normal_config:{spawn_potentials:[{data:{entity:{id:"zombie"}},weight:1}]}}`, ""}, // shown in display order below
		{`vault{config:{key_item:{id:"trial_key",count:1}}}`, `{components:{},config:{key_item:{count:1,id:"minecraft:trial_key"}},id:"minecraft:vault",server_data:{},shared_data:{},x:@X,y:63,z:1}`},
		{`player_head{profile:{name:"Notch"}}`, `{components:{},id:"minecraft:skull",profile:{name:"Notch"},x:@X,y:63,z:1}`},
		{`player_head{profile:"Notch",custom_name:"x"}`, `{components:{},custom_name:"x",id:"minecraft:skull",profile:{name:"Notch"},x:@X,y:63,z:1}`},
		{`chest{lock:{items:["diamond"]}}`, `{Items:[],components:{},id:"minecraft:chest",lock:{items:"minecraft:diamond"},x:@X,y:63,z:1}`},
		{`test_block{mode:"bogus",message:3,powered:1}`, `{components:{},id:"minecraft:test_block",message:"",mode:"fail",powered:1b,x:@X,y:63,z:1}`},
		{`chest{lock:{items:"diamond"}}`, `{Items:[],components:{},id:"minecraft:chest",lock:{items:"minecraft:diamond"},x:@X,y:63,z:1}`},
		{`barrel{LootTable:"chests/simple_dungeon",LootTableSeed:5L}`, `{LootTable:"minecraft:chests/simple_dungeon",LootTableSeed:5L,components:{},id:"minecraft:barrel",x:@X,y:63,z:1}`},
		{`end_gateway{Age:5L,ExactTeleport:1b,exit_portal:[I;1,2,3]}`, `{Age:5L,ExactTeleport:1b,components:{},exit_portal:[I;1,2,3],id:"minecraft:end_gateway",x:@X,y:63,z:1}`},
		{`test_block{mode:"log",message:"hi"}`, `{components:{},id:"minecraft:test_block",message:"hi",mode:"log",powered:0b,x:@X,y:63,z:1}`},
	} {
		x := strconv.Itoa(2 * i)
		c.ChatCommand("setblock " + x + " 63 1 minecraft:" + row.typed)
		c.ExpectCommandFeedback("commands.setblock.success", x, "63", "1")
		if row.want == "" {
			continue
		}
		c.ChatCommand("data get block " + x + " 63 1")
		c.ExpectCommandFeedback("commands.data.block.query", x, "63", "1", strings.ReplaceAll(row.want, "@X", x))
	}

	// The villager's offers go through the item codecs too: a cost always
	// writes its count, a result stack is written back whole.
	srv.RunLine(`summon villager 1 63 1 {Offers:{Recipes:[{buy:{id:"emerald"},buyB:{id:"stick",count:2},sell:{id:"bread"},maxUses:5}]}}`)
	got := plainQuery(t, srv, `data get entity @e[type=villager,limit=1] Offers`, "commands.data.entity.query")
	want := servertest.NBTArg(`{Recipes:[{buy:{count:1,id:"minecraft:emerald"},buyB:{count:2,id:"minecraft:stick"},maxUses:5,sell:{count:1,id:"minecraft:bread"}}]}`).Plain()
	if got != want {
		t.Errorf("villager Offers = %s, want %s", got, want)
	}

	// A compound shows in insertion order where hash buckets tie, which the
	// reparsed SNBT above cannot say: the fields in saveAdditional's order,
	// then components, id and position. The trial spawner (x 2) is the first
	// table's; its ticker fills in the spawn data the load leaves out.
	const spawner = `{components: {}, normal_config: {spawn_potentials: [{data: {entity: {id: "minecraft:zombie"}}, weight: 1}]}, x: 2, y: 63, @Dz: 1, target_cooldown_length: 600, id: "minecraft:trial_spawner"}`
	shown := map[int]string{2: strings.ReplaceAll(spawner, "@D", "")}
	for i, row := range []struct{ typed, want string }{
		{`structure_block{mode:"SAVE",name:"x:y",sizeX:3,sizeY:3,sizeZ:3}`, `{metadata: "", mirror: "NONE", components: {}, ignoreEntities: 1b, powered: 0b, seed: 0L, author: "", rotation: "NONE", posX: 0, mode: "SAVE", posY: 1, sizeX: 3, posZ: 0, integrity: 1.0f, showair: 0b, name: "x:y", x: @X, y: 63, z: 1, id: "minecraft:structure_block", strict: 0b, sizeY: 3, sizeZ: 3, showboundingbox: 1b}`},
		{`suspicious_sand{item:{id:"stick"}}`, `{item: {count: 1, id: "minecraft:stick"}, components: {}, x: @X, y: 63, z: 1, id: "minecraft:brushable_block"}`},
		{`structure_block{mode:"SAVE",ignoreEntities:0,powered:1,integrity:0.5d,posX:90,sizeX:71}`, `{metadata: "", mirror: "NONE", components: {}, ignoreEntities: 0b, powered: 1b, seed: 0L, author: "", rotation: "NONE", posX: 48, mode: "SAVE", posY: 1, sizeX: 48, posZ: 0, integrity: 0.5f, showair: 0b, name: "", x: @X, y: 63, z: 1, id: "minecraft:structure_block", strict: 0b, sizeY: 0, sizeZ: 0, showboundingbox: 1b}`},
		{`test_instance_block{data:{test:"x:y",size:[I;1,2,3],rotation:"180",ignore_entities:1b,status:"cleared"}}`, `{components: {}, data: {test: "x:y", size: [I; 1, 2, 3], ignore_entities: 1b, rotation: "180", status: "cleared"}, x: @X, y: 63, z: 1, id: "minecraft:test_instance_block"}`},
	} {
		x := 24 + 2*i
		srv.RunLine("setblock " + strconv.Itoa(x) + " 63 1 minecraft:" + row.typed)
		shown[x] = strings.ReplaceAll(row.want, "@X", strconv.Itoa(x))
	}
	expectShown := func(srv *server.Server, when string) {
		t.Helper()
		for x, want := range shown {
			if got := plainQuery(t, srv, "data get block "+strconv.Itoa(x)+" 63 1", "commands.data.block.query"); got != want {
				t.Errorf("%s, the block at x %d shows %s, want %s", when, x, got, want)
			}
		}
	}
	expectShown(srv, "placed")
	servertest.RunTicks(t, srv, 1)
	shown[2] = strings.ReplaceAll(spawner, "@D", `spawn_data: {entity: {id: "minecraft:zombie"}}, `)
	expectShown(srv, "after a tick")

	// A chunk save and load keeps none of the order; the read writes it again.
	c.Conn.Close()
	srv.Shutdown()
	restarted := *cfg
	back, _ := servertest.Join(t, &restarted, "Setter", servertest.OpsListing(t, "Setter"), freeze)
	servertest.HoldTicks(t, back)
	expectShown(back, "after a restart")
}

// plainQuery runs a /data get and returns its tag as the SNBT the client shows.
func plainQuery(t *testing.T, srv *server.Server, line, key string) string {
	t.Helper()
	for _, text := range srv.RunLine(line) {
		if text.Translate == key && len(text.With) > 0 {
			return text.With[len(text.With)-1].Plain()
		}
	}
	t.Fatalf("%s answered %+v", line, srv.RunLine(line))
	return ""
}
