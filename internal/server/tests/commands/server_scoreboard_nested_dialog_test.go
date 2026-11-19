package commands

import (
	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/internal/server/servertest"
	"testing"
)

// vanilla: DialogBody.COMPACT_LIST_CODEC, Action.CODEC, Input.CODEC, HolderSetCodec.
// Literal messages were recorded from the registry-aware vanilla 26.3 codec.
func TestScoreboardNestedDialogCodecDiagnostics(t *testing.T) {
	t.Parallel()
	srv, c := joinMinimalOp(t)
	c.ChatCommand("scoreboard objectives add kills dummy")
	c.ExpectWithinTicks(srv, 0, protocol.IDPlaySystemChat)
	for _, mode := range []struct{ prefix, key, start string }{
		{"scoreboard objectives add bad dummy ", "argument.component.invalid", `{text:"x",`},
		{"scoreboard objectives modify kills numberformat styled ", "argument.style.invalid", `{`},
	} {
		for _, tc := range []struct{ dialog, want string }{
			{`{type:"notice",title:"x",pause:"x",after_action:"none"}`, `Not a number; Dialogs that pause the game must use after_action values that unpause it after user action!`},
			{`{type:"dialog_list",title:"x",dialogs:{type:"notice",title:"x",after_action:"none"}}`, `Failed to parse either. First: Not a string; Second: Failed to parse either. First: Not a list: {after_action:"none",title:"x",type:"notice"}; Second: Dialogs that pause the game must use after_action values that unpause it after user action!`},
			{`{type:"dialog_list",title:"x",dialogs:{type:"notice",title:"x",pause:"x"}}`, `Not a number`},
			{`{type:"dialog_list",title:"x",dialogs:{type:"notice",title:5}}`, `Failed to parse either. First: Not a string; Second: Failed to parse either. First: Not a list: {title:5,type:"notice"}; Second: Failed to parse either. First: Failed to parse either. First: Not a string; Second: Not a list: 5; Second: Not a map: 5`},
			{`{type:"notice",title:"x",body:{type:"bad"}}`, `Failed to parse either. First: Not a list: {type:"bad"}; Second: Unknown registry key in ResourceKey[minecraft:root / minecraft:dialog_body_type]: minecraft:bad`},
			{`{type:"notice",title:"x",body:5}`, `Failed to parse either. First: Not a list: 5; Second: Not a map: 5`},
			{`{type:"notice",title:"x",body:{}}`, `Failed to parse either. First: Not a list: {}; Second: No key type in MapLike[{}]`},
			{`{type:"notice",title:"x",body:{type:5}}`, `Failed to parse either. First: Not a list: {type:5}; Second: Not a string`},
			{`{type:"notice",title:"x",body:[{type:"bad"},5]}`, `Not a map: 5; Unknown registry key in ResourceKey[minecraft:root / minecraft:dialog_body_type]: minecraft:bad`},
			{`{type:"notice",title:"x",body:{type:"plain_message",contents:"x",width:0}}`, `Value must be within range [1;1024]: 0`},
			{`{type:"notice",title:"x",body:{type:"plain_message",contents:5,width:0}}`, `Failed to parse either. First: Not a list: {contents:5,type:"plain_message",width:0}; Second: Value must be within range [1;1024]: 0; Failed to parse either. First: Failed to parse either. First: Not a string; Second: Not a list: 5; Second: Not a map: 5`},
			{`{type:"notice",title:"x",body:{type:"item",item:"missing"}}`, `Failed to parse either. First: Not a list: {item:"missing",type:"item"}; Second: Failed to parse either. First: Not a map: "missing"; Second: Unknown registry key in ResourceKey[minecraft:root / minecraft:item]: minecraft:missing`},
			{`{type:"notice",title:"x",body:{type:"item",item:{id:"stone",count:0,components:5}}}`, `Not a map: 5; Value must be within range [1;99]: 0`},
			{`{type:"notice",title:"x",body:{type:"item",item:5}}`, `Failed to parse either. First: Not a list: {item:5,type:"item"}; Second: Failed to parse either. First: Not a map: 5; Second: Not a string`},
			{`{type:"notice",title:"x",body:{type:"item",item:{}}}`, `Failed to parse either. First: Not a list: {item:{},type:"item"}; Second: Failed to parse either. First: No key id in MapLike[{}]; Second: Not a string`},
			{`{type:"notice",title:"x",body:{type:"item",item:"stone",description:{contents:"x",width:0}}}`, `Value must be within range [1;1024]: 0`},
			{`{type:"notice",title:"x",body:{type:"item",item:"stone",description:{}}}`, `Failed to parse either. First: No key contents in MapLike[{}]; Second: Failed to parse either. First: Failed to parse either. First: Not a string; Second: Not a list: {}; Second: No matching codec found`},
			{`{type:"notice",title:"x",body:{type:"item",item:"stone",width:0,height:0,show_tooltip:"x",show_decorations:"x"}}`, `Value must be within range [1;256]: 0; Value must be within range [1;256]: 0; Not a number; Not a number`},
			{`{type:"notice",title:"x",action:{label:"x",action:{type:"open_url",url:"ftp://x"}}}`, `Unsupported protocol in URI: ftp://x: ftp://x`},
			{`{type:"notice",title:"x",action:{label:"x",action:{type:"open_url"}}}`, `No key url in MapLike[{type:"open_url"}]`},
			{`{type:"notice",title:"x",action:{label:"x",action:{type:"suggest_command",command:5}}}`, `Not a string`},
			{`{type:"notice",title:"x",action:{label:"x",action:{type:"custom",id:"BAD"}}}`, `Not a valid resource location: BAD Non [a-z0-9/._-] character in path of location: minecraft:BAD`},
			{`{type:"notice",title:"x",action:{label:"x",action:{type:"bad"}}}`, `Unknown registry key in ResourceKey[minecraft:root / minecraft:dialog_action_type]: minecraft:bad`},
			{`{type:"notice",title:"x",action:{label:"x",action:{type:"dynamic/custom",id:5,additions:1}}}`, `Not a compound tag: 1; Not a string`},
			{`{type:"notice",title:"x",action:{label:"x",action:{type:"dynamic/run_command"}}}`, `No key template in MapLike[{type:"dynamic/run_command"}]`},
			{`{type:"notice",title:"x",action:{label:"x",action:{type:"change_page",page:4294967296L}}}`, `Value must be positive: 0`},
			{`{type:"notice",title:"x",action:{label:"x",action:{}}}`, `No key type in MapLike[{}]`},
			{`{type:"dialog_list",title:"x",dialogs:5}`, `Failed to parse either. First: Not a string; Second: Failed to parse either. First: Not a list: 5; Second: Not a map: 5`},
			{`{type:"dialog_list",title:"x",dialogs:"missing"}`, `Failed to parse either. First: Not a tag id; Second: Failed to parse either. First: Not a list: "missing"; Second: Failed to get element minecraft:missing from registry minecraft:dialog`},
			{`{type:"dialog_list",title:"x",dialogs:["missing","other"]}`, `Failed to get element minecraft:other from registry minecraft:dialog; Failed to get element minecraft:missing from registry minecraft:dialog`},
			{`{type:"dialog_list",title:"x",dialogs:[{type:"notice"}]}`, `No key title in MapLike[{type:"notice"}]`},
			{`{type:"dialog_list",title:"x",dialogs:"#BAD"}`, `Failed to parse either. First: Not a valid resource location: BAD Non [a-z0-9/._-] character in path of location: minecraft:BAD; Second: Failed to parse either. First: Not a list: "#BAD"; Second: Not a map: "#BAD"`},
			{`{type:"dialog_list",title:"x",dialogs:["#quick_actions"]}`, `Not a map: "#quick_actions"`},
			{`{type:"dialog_list",title:"x",columns:"x",button_width:0,exit_action:{width:5},dialogs:[]}`, `Value must be within range [1;1024]: 0; Not a number; No key label in MapLike[{width:5}]`},
			{`{type:"notice",title:"x",inputs:[{}]}`, `No key type in MapLike[{}]; No key key in MapLike[{}]`},
			{`{type:"notice",title:"x",inputs:[5,{}]}`, `No key type in MapLike[{}]; No key key in MapLike[{}]; Not a map: 5`},
			{`{type:"notice",title:"x",inputs:[{type:"bad",key:5}]}`, `Unknown registry key in ResourceKey[minecraft:root / minecraft:input_control_type]: minecraft:bad; Not a string`},
			{`{type:"notice",title:"x",inputs:[{type:"number_range",key:"k",label:"l",end:1}]}`, `No key start in MapLike[{end:1,key:"k",label:"l",type:"number_range"}]`},
			{`{type:"notice",title:"x",inputs:[{type:"boolean",key:5,label:"l"}]}`, `Not a string`},
			{`{type:"notice",title:"x",inputs:[{type:"boolean",key:"a b",label:"l"}]}`, `a b is not a valid input name`},
		} {
			input := mode.prefix + mode.start + `click_event:{action:"show_dialog",dialog:` + tc.dialog + `}}`
			c.ChatCommand(input)
			expectCommandErrorWithinTicks(srv, c,
				servertest.CommandErrorMessageForTest(mode.key, tc.want),
				servertest.CommandErrorContextForTest(input, len(mode.prefix)))
		}
	}
}

// vanilla: HolderSetCodec.decode permits direct and reference holders together.
func TestScoreboardDialogAllowsMixedHolders(t *testing.T) {
	t.Parallel()
	srv, c := joinMinimalOp(t)
	c.ChatCommand("scoreboard objectives add kills dummy")
	c.ExpectWithinTicks(srv, 0, protocol.IDPlaySystemChat)
	c.ChatCommand(`scoreboard objectives modify kills numberformat styled {click_event:{action:"show_dialog",dialog:{type:"dialog_list",title:"x",dialogs:["server_links",{type:"notice",title:"x"}]}}}`)
	expectScoreboardFeedback(t, c, "commands.scoreboard.objectives.modify.objectiveFormat.set")
}
