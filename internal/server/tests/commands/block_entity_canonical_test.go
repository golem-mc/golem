package commands

import (
	"strconv"
	"strings"
	"testing"

	"github.com/golem-mc/golem/internal/config"
	"github.com/golem-mc/golem/internal/protocol"
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
	_, c := servertest.Join(t, cfg, "Setter", servertest.OpsListing(t, "Setter"))

	for i, row := range []struct{ typed, want string }{
		{`white_banner{patterns:[{pattern:"creeper",color:"red"}],CustomName:"B"}`, `{CustomName:"B",components:{},id:"minecraft:banner",patterns:[{color:"red",pattern:"minecraft:creeper"}],x:@X,y:63,z:1}`},
		{`structure_block{mode:"SAVE",name:"x:y",sizeX:3,sizeY:3,sizeZ:3}`, `{author:"",components:{},id:"minecraft:structure_block",ignoreEntities:1b,integrity:1.0f,metadata:"",mirror:"NONE",mode:"SAVE",name:"x:y",posX:0,posY:1,posZ:0,powered:0b,rotation:"NONE",seed:0L,showair:0b,showboundingbox:1b,sizeX:3,sizeY:3,sizeZ:3,strict:0b,x:@X,y:63,z:1}`},
		{`trial_spawner{normal_config:{spawn_potentials:[{data:{entity:{id:"zombie"}},weight:1}]}}`, `{components:{},id:"minecraft:trial_spawner",normal_config:{spawn_potentials:[{data:{entity:{id:"minecraft:zombie"}},weight:1}]},x:@X,y:63,z:1}`},
		{`vault{config:{key_item:{id:"trial_key",count:1}}}`, `{components:{},config:{key_item:{count:1,id:"minecraft:trial_key"}},id:"minecraft:vault",server_data:{},shared_data:{},x:@X,y:63,z:1}`},
		{`player_head{profile:{name:"Notch"}}`, `{components:{},id:"minecraft:skull",profile:{name:"Notch"},x:@X,y:63,z:1}`},
		{`player_head{profile:"Notch",custom_name:"x"}`, `{components:{},custom_name:"x",id:"minecraft:skull",profile:{name:"Notch"},x:@X,y:63,z:1}`},
		{`chest{lock:{items:"diamond"}}`, `{Items:[],components:{},id:"minecraft:chest",lock:{items:"minecraft:diamond"},x:@X,y:63,z:1}`},
		{`barrel{LootTable:"chests/simple_dungeon",LootTableSeed:5L}`, `{LootTable:"minecraft:chests/simple_dungeon",LootTableSeed:5L,components:{},id:"minecraft:barrel",x:@X,y:63,z:1}`},
		{`end_gateway{Age:5L,ExactTeleport:1b,exit_portal:[I;1,2,3]}`, `{Age:5L,ExactTeleport:1b,components:{},exit_portal:[I;1,2,3],id:"minecraft:end_gateway",x:@X,y:63,z:1}`},
		{`suspicious_sand{item:{id:"diamond",count:1}}`, `{components:{},id:"minecraft:brushable_block",item:{count:1,id:"minecraft:diamond"},x:@X,y:63,z:1}`},
		{`test_block{mode:"log",message:"hi"}`, `{components:{},id:"minecraft:test_block",message:"hi",mode:"log",powered:0b,x:@X,y:63,z:1}`},
		{`test_instance_block{data:{test:"x:y"}}`, `{components:{},data:{test:"x:y"},id:"minecraft:test_instance_block",x:@X,y:63,z:1}`},
	} {
		x := strconv.Itoa(2 * i)
		c.ChatCommand("setblock " + x + " 63 1 minecraft:" + row.typed)
		c.ExpectCommandFeedback("commands.setblock.success", x, "63", "1")
		c.ChatCommand("data get block " + x + " 63 1")
		c.ExpectCommandFeedback("commands.data.block.query", x, "63", "1", strings.ReplaceAll(row.want, "@X", x))
	}

	// The villager's offers go through the item codecs too.
	c.ChatCommand(`summon villager 1 63 1 {Offers:{Recipes:[{buy:{id:"emerald",count:1},sell:{id:"bread",count:2},maxUses:5}]}}`)
	c.ExpectPlay(protocol.IDPlaySystemChat) // the summon's feedback
	one := int64(1)
	// execute-if is how a command tests the vanilla form.
	c.ChatCommand(`execute if data entity @e[type=villager,limit=1] Offers.Recipes[0].buy{id:"minecraft:emerald"}`)
	c.ExpectCommandFeedbackText("commands.execute.conditional.pass_count", protocol.Text{Number: &one})
	c.ChatCommand(`execute if data entity @e[type=villager,limit=1] Offers.Recipes[0].sell{id:"minecraft:bread"}`)
	c.ExpectCommandFeedbackText("commands.execute.conditional.pass_count", protocol.Text{Number: &one})
	c.Disconnect()
}
