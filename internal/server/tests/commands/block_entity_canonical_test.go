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
	cfg := config.Default()
	cfg.OnlineMode = false
	cfg.GameMode = "creative"
	// Held, so no ticker (gateway age, vault data, trial spawner) rewrites
	// the entity between /setblock and /data get.
	srv, c := servertest.Join(t, cfg, "Setter", servertest.OpsListing(t, "Setter"), func(s *server.Server) { servertest.FreezeUntilHeld(t, s) })
	servertest.HoldTicks(t, srv)

	for i, row := range []struct{ typed, want string }{
		{`white_banner{patterns:[{pattern:"creeper",color:"red"}],CustomName:"B"}`, `{CustomName:"B",components:{},id:"minecraft:banner",patterns:[{color:"red",pattern:"minecraft:creeper"}],x:@X,y:63,z:1}`},
		{`trial_spawner{normal_config:{spawn_potentials:[{data:{entity:{id:"zombie"}},weight:1}]}}`, `{components:{},id:"minecraft:trial_spawner",normal_config:{spawn_potentials:[{data:{entity:{id:"minecraft:zombie"}},weight:1}]},x:@X,y:63,z:1}`},
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
	// then components, id and position.
	for _, row := range []struct{ typed, want string }{
		{`structure_block{mode:"SAVE",name:"x:y",sizeX:3,sizeY:3,sizeZ:3}`, `{metadata: "", mirror: "NONE", components: {}, ignoreEntities: 1b, powered: 0b, seed: 0L, author: "", rotation: "NONE", posX: 0, mode: "SAVE", posY: 1, sizeX: 3, posZ: 0, integrity: 1.0f, showair: 0b, name: "x:y", x: 40, y: 63, z: 1, id: "minecraft:structure_block", strict: 0b, sizeY: 3, sizeZ: 3, showboundingbox: 1b}`},
		{`test_instance_block{data:{test:"x:y",size:[I;1,2,3],rotation:"180",ignore_entities:1b,status:"cleared"}}`, `{components: {}, data: {test: "x:y", size: [I; 1, 2, 3], ignore_entities: 1b, rotation: "180", status: "cleared"}, x: 40, y: 63, z: 1, id: "minecraft:test_instance_block"}`},
	} {
		srv.RunLine("setblock 40 63 1 minecraft:" + row.typed)
		if got := plainQuery(t, srv, "data get block 40 63 1", "commands.data.block.query"); got != row.want {
			t.Errorf("%s shows %s, want %s", row.typed, got, row.want)
		}
	}

	// The trial spawner's ticker fills in the spawn data the load leaves out.
	srv.RunLine(`setblock 42 63 1 minecraft:trial_spawner{normal_config:{spawn_potentials:[{data:{entity:{id:"zombie"}},weight:1}]}}`)
	servertest.RunTicks(t, srv, 40)
	got = plainQuery(t, srv, "data get block 42 63 1 spawn_data", "commands.data.block.query")
	if want := servertest.NBTArg(`{entity:{id:"minecraft:zombie"}}`).Plain(); got != want {
		t.Errorf("trial spawner spawn_data = %s, want %s", got, want)
	}
	c.Disconnect()
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
