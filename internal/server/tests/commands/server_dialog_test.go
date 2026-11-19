package commands

import (
	"bytes"
	"testing"

	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/internal/server/servertest"
	"github.com/golem-mc/golem/nbt"
)

// sawAfterSettle reports whether a packet with the id arrived before the
// server answered a ping, so everything sent earlier has been acted on.
func sawAfterSettle(c *servertest.Client, id int32) bool {
	c.T.Helper()
	saw := false
	c.SettleAndCollect(func(got int32, _ []byte) {
		if got == id {
			saw = true
		}
	})
	return saw
}

// TestDialogShowAndClearReachTheSelectedPlayers checks that /dialog show
// sends one Show Dialog packet naming the registry entry to exactly the
// selected players, and /dialog clear a Clear Dialog packet to each.
// There is no client in this environment, so the layout is checked against
// ByteBufCodecs.holder alone: the entry's position plus one.
// vanilla: DialogCommand, ClientboundShowDialogPacket
func TestDialogShowAndClearReachTheSelectedPlayers(t *testing.T) {
	t.Parallel()
	_, alice, bob, _ := servertest.StartExecuteWorldForTwo(t, "Alice", "Bob")

	alice.ChatCommand("dialog show Bob minecraft:server_links")
	data, ok := servertest.ReadUntil(t, bob, servertest.ExpectTimeout, protocol.IDPlayShowDialog)
	if !ok {
		t.Fatal("Bob got no Show Dialog packet")
	}
	// minecraft:server_links is the third entry of the registry.
	if want := []byte{3}; !bytes.Equal(data, want) {
		t.Errorf("Show Dialog = % x, want % x", data, want)
	}
	servertest.AwaitCommandFeedbackText(t, alice, "commands.dialog.show.single", servertest.PlayerDisplayComponent("Bob"))
	if sawAfterSettle(alice, protocol.IDPlayShowDialog) {
		t.Error("the runner got a dialog nobody selected them for")
	}

	alice.ChatCommand("dialog clear @a")
	for name, c := range map[string]*servertest.Client{"Alice": alice, "Bob": bob} {
		if _, ok := servertest.ReadUntil(t, c, servertest.ExpectTimeout, protocol.IDPlayClearDialog); !ok {
			t.Errorf("%s got no Clear Dialog packet", name)
		}
	}
	servertest.AwaitCommandFeedbackText(t, alice, "commands.dialog.clear.multiple", protocol.Text{Text: "2"})
	alice.Disconnect()
	bob.Disconnect()
}

// TestDialogShowInlineSendsTheCompound checks that an inline dialog goes out
// as 0 and the network NBT of the compound.
// vanilla: ByteBufCodecs.holder
func TestDialogShowInlineSendsTheCompound(t *testing.T) {
	t.Parallel()
	_, alice, bob, _ := servertest.StartExecuteWorldForTwo(t, "Alice", "Bob")

	alice.ChatCommand(`dialog show Bob {type:"minecraft:notice",title:"Hi"}`)
	data, ok := servertest.ReadUntil(t, bob, servertest.ExpectTimeout, protocol.IDPlayShowDialog)
	if !ok {
		t.Fatal("Bob got no Show Dialog packet")
	}
	if len(data) == 0 || data[0] != 0 {
		t.Fatalf("Show Dialog = % x, want the inline marker 0 first", data)
	}
	tag, _, err := nbt.DecodeNetwork(data[1:])
	if err != nil {
		t.Fatal(err)
	}
	c, _ := tag.(nbt.Compound)
	if c["type"] != nbt.String("minecraft:notice") || c["title"] != nbt.String("Hi") {
		t.Errorf("inline dialog = %v", tag)
	}
	alice.Disconnect()
	bob.Disconnect()
}

// TestDialogResultIsTheTargetCount checks that /execute store result takes
// the number of selected players, not 1.
// vanilla: DialogCommand.showDialog
func TestDialogResultIsTheTargetCount(t *testing.T) {
	t.Parallel()
	_, alice, bob, _ := servertest.StartExecuteWorldForTwo(t, "Alice", "Bob")

	alice.ChatCommand("execute store result storage t:s r int 1 run dialog show @a minecraft:server_links")
	if _, ok := servertest.ReadUntil(t, alice, servertest.ExpectTimeout, protocol.IDPlayShowDialog); !ok {
		t.Fatal("Alice got no Show Dialog packet")
	}
	servertest.AwaitCommandFeedbackText(t, alice, "commands.dialog.show.multiple", protocol.Text{Text: "2"})
	alice.ChatCommand("data get storage t:s r")
	alice.ExpectCommandFeedback("commands.data.storage.query", "t:s", "2")
	alice.Disconnect()
	bob.Disconnect()
}

// TestDialogRefusals checks the unknown dialog, a type no dialog has, an inline dialog missing a required key, a
// selector that matches nobody and a runner without the permission.
// vanilla: ResourceOrIdArgument, EntityArgument.getPlayers
func TestDialogRefusals(t *testing.T) {
	t.Parallel()
	_, alice, bob, _ := servertest.StartExecuteWorldForTwo(t, "Alice", "Bob")

	input := "dialog show Bob minecraft:nope"
	alice.ChatCommand(input)
	alice.ExpectCommandError(
		servertest.CommandErrorMessageForTest("argument.resource_or_id.no_such_element", "minecraft:nope", "minecraft:dialog"),
		servertest.CommandErrorContextForTest(input, len(input)),
	)
	input = `dialog show Bob {type:"minecraft:notice"}`
	alice.ChatCommand(input)
	alice.ExpectCommandError(
		servertest.CommandErrorMessageForTest("argument.resource_or_id.failed_to_parse", `No key title in MapLike[{type:"minecraft:notice"}]`),
		servertest.CommandErrorContextForTest(input, len(input)),
	)
	input = `dialog show Bob {type:"minecraft:confirmation",title:"Hi",yes:{label:"Y"}}`
	alice.ChatCommand(input)
	alice.ExpectCommandError(
		servertest.CommandErrorMessageForTest("argument.resource_or_id.failed_to_parse", `No key no in MapLike[{title:"Hi",type:"minecraft:confirmation",yes:{label:"Y"}}]`),
		servertest.CommandErrorContextForTest(input, len(input)),
	)
	input = `dialog show Bob {type:"minecraft:bogus"}`
	alice.ChatCommand(input)
	alice.ExpectCommandError(
		servertest.CommandErrorMessageForTest("argument.resource_or_id.failed_to_parse", "Unknown dialog type minecraft:bogus"),
		servertest.CommandErrorContextForTest(input, len(input)),
	)
	for _, input := range []string{"dialog show Nobody minecraft:server_links", "dialog clear Nobody"} {
		alice.ChatCommand(input)
		alice.ExpectCommandFailure(servertest.CommandErrorMessageForTest("argument.entity.notfound.player"))
	}

	input = "dialog show Alice minecraft:server_links"
	bob.ChatCommand(input)
	bob.ExpectCommandError(
		servertest.CommandErrorMessageForTest("command.unknown.command"),
		servertest.CommandErrorContextForTest(input, 0),
	)
	if sawAfterSettle(alice, protocol.IDPlayShowDialog) {
		t.Error("a refused command still showed a dialog")
	}
	alice.Disconnect()
	bob.Disconnect()
}

// TestCustomClickActionRunsNothing checks that a run_command payload from a
// player without permission is only logged: vanilla's handler does no more.
// vanilla: MinecraftServer.handleCustomClickAction
func TestCustomClickActionRunsNothing(t *testing.T) {
	t.Parallel()
	_, alice, bob, _ := servertest.StartExecuteWorldForTwo(t, "Alice", "Bob")

	payload, err := nbt.EncodeNetwork(nbt.Compound{"command": nbt.String("dialog show Alice minecraft:server_links")})
	if err != nil {
		t.Fatal(err)
	}
	bob.Send(protocol.IDPlayCustomClickAction, func(b *protocol.Buffer) {
		b.PutString("minecraft:run_command")
		b.PutVarInt(int32(len(payload)))
		b.PutRaw(payload)
	})
	bob.Settle()
	if sawAfterSettle(alice, protocol.IDPlayShowDialog) {
		t.Error("a click action ran a command")
	}
	alice.Disconnect()
	bob.Disconnect()
}

// TestDialogInlineFieldsAreValidated checks that an inline dialog whose
// fields the codec refuses is not sent, and that a well-formed multi_action
// dialog is.
// vanilla: Dialog.DIRECT_CODEC
func TestDialogInlineFieldsAreValidated(t *testing.T) {
	t.Parallel()
	_, alice, bob, _ := servertest.StartExecuteWorldForTwo(t, "Alice", "Bob")

	for _, tc := range []struct{ dialog, msg string }{
		{`{type:"minecraft:multi_action",title:"x",actions:[]}`, "List must have contents"},
		{`{type:"minecraft:multi_action",title:"x",actions:[{width:5}]}`, "No key label in MapLike[{width:5}]; List must have contents"},
		{`{type:"minecraft:multi_action",title:"x",actions:[{label:"a"}],columns:0}`, "Value must be positive: 0"},
		{`{type:"minecraft:notice",title:"x",action:{label:"a",width:2000}}`, "Value must be within range [1;1024]: 2000"},
		{`{type:"minecraft:notice",title:"x",body:{type:"minecraft:bogus"}}`, `Failed to parse either. First: Not a list: {type:"minecraft:bogus"}; Second: Unknown registry key in ResourceKey[minecraft:root / minecraft:dialog_body_type]: minecraft:bogus`},
		{`{type:"minecraft:notice",title:"x",after_action:"none"}`, "Dialogs that pause the game must use after_action values that unpause it after user action!"},
		{`{type:"minecraft:notice",title:5}`, `Failed to parse either. First: Failed to parse either. First: Not a string; Second: Not a list: 5; Second: Not a map: 5`},
		{`{type:"minecraft:dialog_list",title:"x",dialogs:["minecraft:nope"]}`, "Failed to get element minecraft:nope from registry minecraft:dialog"},
		{`{type:"minecraft:server_links",title:"x",button_width:0}`, "Value must be within range [1;1024]: 0"},
		{`{type:"minecraft:server_links",title:"x",columns:0}`, "Value must be positive: 0"},
		{`{type:"minecraft:confirmation",title:"x",yes:{label:"a"}}`, "No key no in MapLike[{title:\"x\",type:\"minecraft:confirmation\",yes:{label:\"a\"}}]"},
		{`{type:"minecraft:confirmation",title:"x",yes:{label:"a"},no:{label:"b",width:0}}`, "Value must be within range [1;1024]: 0"},
		{`{type:"minecraft:notice",title:"x",pause:256,after_action:"none"}`, "Dialogs that pause the game must use after_action values that unpause it after user action!"},
		{`{type:"minecraft:notice",title:"x",inputs:[{type:"minecraft:single_option",key:"k",label:"l",options:[{id:"a",initial:256},{id:"b",initial:256}]}]}`, "Multiple initial values"},
		{`{type:"minecraft:notice",title:"x",pause:"yes"}`, `Not a number`},
		{`{type:"minecraft:notice",title:"x",action:{label:"a",action:{type:"minecraft:bogus"}}}`, `Unknown registry key in ResourceKey[minecraft:root / minecraft:dialog_action_type]: minecraft:bogus`},
		{`{type:"minecraft:notice",title:"x",body:{type:"minecraft:item",item:"minecraft:nope"}}`, `Failed to parse either. First: Not a list: {item:"minecraft:nope",type:"minecraft:item"}; Second: Failed to parse either. First: Not a map: "minecraft:nope"; Second: Unknown registry key in ResourceKey[minecraft:root / minecraft:item]: minecraft:nope`},
		{`{type:"minecraft:notice",title:"x",body:{type:"minecraft:item",item:{id:"minecraft:stone",count:100}}}`, "Value must be within range [1;99]: 100"},
		{`{type:"minecraft:notice",title:"x",inputs:[{type:"minecraft:text",key:"a b",label:"l"}]}`, "a b is not a valid input name"},
		{`{type:"minecraft:notice",title:"x",inputs:[{type:"minecraft:single_option",key:"k",label:"l",options:[]}]}`, "List must have contents"},
		{`{type:"minecraft:notice",title:"x",inputs:[{type:"minecraft:single_option",key:"k",label:"l",options:[{id:"a",initial:1b},{id:"b",initial:1b}]}]}`, "Multiple initial values"},
		{`{type:"minecraft:notice",title:"x",inputs:[{type:"minecraft:text",key:"k",label:"l",max_length:0}]}`, "Value must be positive: 0"},
		{`{type:"minecraft:notice",title:"x",inputs:[{type:"minecraft:text",key:"k",label:"l",max_length:2,initial:"abc"}]}`, "Default text length exceeds allowed size"},
		{`{type:"minecraft:notice",title:"x",inputs:[{type:"minecraft:text",key:"k",label:"l",multiline:{height:600}}]}`, "Value must be within range [1;512]: 600"},
		{`{type:"minecraft:notice",title:"x",inputs:[{type:"minecraft:number_range",key:"k",label:"l",end:1}]}`, `No key start in MapLike[{end:1,key:"k",label:"l",type:"minecraft:number_range"}]`},
		{`{type:"minecraft:notice",title:"x",inputs:[{type:"minecraft:number_range",key:"k",label:"l",start:0,end:1,step:-1f}]}`, "Value must be positive: -1.0f"},
		{`{type:"minecraft:notice",title:"x",inputs:[{type:"minecraft:number_range",key:"k",label:"l",start:0,end:1,initial:5}]}`, "Initial value 5 is outside of range [0, 1]"},
		{`{type:"minecraft:notice",title:"x",body:{type:"minecraft:item",item:"minecraft:air"}}`, `Failed to parse either. First: Not a list: {item:"minecraft:air",type:"minecraft:item"}; Second: Failed to parse either. First: Not a map: "minecraft:air"; Second: Item must not be minecraft:air`},
		{`{type:"minecraft:notice",title:"x",action:{label:"a",action:{type:"open_url"}}}`, `No key url in MapLike[{type:"open_url"}]`},
		{`{type:"minecraft:notice",title:"x",action:{label:"a",action:{type:"open_url",url:"ftp://x"}}}`, `Unsupported protocol in URI: ftp://x: ftp://x`},
		{`{type:"minecraft:notice",title:"x",action:{label:"a",action:{type:"run_command"}}}`, `No key command in MapLike[{type:"run_command"}]`},
		{`{type:"minecraft:notice",title:"x",action:{label:"a",action:{type:"suggest_command",command:5}}}`, `Not a string`},
		{`{type:"minecraft:notice",title:"x",action:{label:"a",action:{type:"copy_to_clipboard"}}}`, `No key value in MapLike[{type:"copy_to_clipboard"}]`},
		{`{type:"minecraft:notice",title:"x",action:{label:"a",action:{type:"change_page",page:0}}}`, `Value must be positive: 0`},
		{`{type:"minecraft:notice",title:"x",action:{label:"a",action:{type:"show_dialog"}}}`, `No key dialog in MapLike[{type:"show_dialog"}]`},
		{`{type:"minecraft:notice",title:"x",action:{label:"a",action:{type:"show_dialog",dialog:"minecraft:nope"}}}`, `Failed to get element minecraft:nope from registry minecraft:dialog`},
		{`{type:"minecraft:notice",title:"x",action:{label:"a",action:{type:"custom",id:"Bad Id"}}}`, `Not a valid resource location: Bad Id Non [a-z0-9/._-] character in path of location: minecraft:Bad Id`},
		{`{type:"minecraft:notice",title:"x",action:{label:"a",action:{type:"custom"}}}`, `No key id in MapLike[{type:"custom"}]`},
		{`{type:"minecraft:notice",title:"x",action:{label:"a",action:{type:"dynamic/run_command"}}}`, `No key template in MapLike[{type:"dynamic/run_command"}]`},
		{`{type:"minecraft:notice",title:"x",action:{label:"a",action:{type:"dynamic/custom",id:"a:b",additions:1}}}`, `Not a compound tag: 1`},
		{`{type:"minecraft:notice",title:"x",action:{label:"a",action:{type:"open_url",url:"https://exa mple.com"}}}`, `Illegal character in authority at index 11: https://exa mple.com`},
		{`{type:"minecraft:notice",title:"x",action:{label:"a",action:{type:"show_dialog",dialog:"#minecraft:quick_actions"}}}`, `Not a map: "#minecraft:quick_actions"`},
		{`{type:"minecraft:dialog_list",title:"x",dialogs:"#minecraft:nope"}`, `Missing tag: 'minecraft:nope' in 'minecraft:dialog'`},
		{`{type:"minecraft:server_links",title:"x",columns:4294967296L}`, `Value must be positive: 0`},
		{`{type:"minecraft:notice",title:"x",inputs:[{type:"minecraft:number_range",key:"k",label:"l",start:0,end:1,step:1e-50d}]}`, "Value must be positive: 1.0E-50d"},
		{`{type:"minecraft:notice",title:"x",inputs:[{type:"minecraft:number_range",key:"k",label:"l",start:0,end:1,step:1e39d}]}`, "Value must be positive: Infinity"},
		{`{type:"minecraft:notice",title:{selector:"@e["}}`, textFail(`{selector:"@e["}`)},
		{`{type:"minecraft:notice",title:{nbt:"a[",storage:"a:b"}}`, textFail(`{nbt:"a[",storage:"a:b"}`)},
		{`{type:"minecraft:notice",title:{nbt:"a",storage:"a:b",interpret:1b,plain:1b}}`, textFail(`{interpret:1b,nbt:"a",plain:1b,storage:"a:b"}`)},
		{`{type:"minecraft:notice",title:{selector:"@e",separator:{}}}`, textFail(`{selector:"@e",separator:{}}`)},
		{`{type:"minecraft:dialog_list",title:"x",dialogs:["#minecraft:quick_actions"]}`, `Not a map: "#minecraft:quick_actions"`},
		{`{type:"minecraft:notice",title:"x",action:{label:"a",action:{type:"dynamic/run_command",template:"say hi"}}}`, `Failed to parse template say hi: No variables in macro`},
		{`{type:"minecraft:notice",title:"x",action:{label:"a",action:{type:"dynamic/run_command",template:"say $(a"}}}`, `Failed to parse template say $(a: Unterminated macro variable`},
		{`{type:"minecraft:notice",title:"x",action:{label:"a",action:{type:"dynamic/run_command",template:"say $(a b)"}}}`, `Failed to parse template say $(a b): Invalid macro variable name 'a b'`},
		{`{type:"minecraft:notice",title:{}}`, `Failed to parse either. First: Failed to parse either. First: Not a string; Second: Not a list: {}; Second: No matching codec found`},
		{`{type:"minecraft:notice",title:{text:"x",click_event:{action:"open_url"}}}`, `No key url in MapLike[{action:"open_url"}]`},
		{`{type:"minecraft:notice",title:"x",external_title:{}}`, `Failed to parse either. First: Failed to parse either. First: Not a string; Second: Not a list: {}; Second: No matching codec found`},
		{`{type:"minecraft:notice",title:"x",body:{type:"minecraft:plain_message",contents:{}}}`, `Failed to parse either. First: Not a list: {contents:{},type:"minecraft:plain_message"}; Second: Failed to parse either. First: Failed to parse either. First: Not a string; Second: Not a list: {}; Second: No matching codec found`},
		{`{type:"minecraft:notice",title:"x",action:{label:{},action:{type:"change_page",page:1}}}`, `Failed to parse either. First: Failed to parse either. First: Not a string; Second: Not a list: {}; Second: No matching codec found`},
		{`{type:"minecraft:notice",title:"x",action:{label:"a",tooltip:{text:"x",hover_event:{action:"show_text"}}}}`, `No key value in MapLike[{action:"show_text"}]`},
		{`{type:"minecraft:notice",title:"x",inputs:[{type:"minecraft:boolean",key:"k",label:{}}]}`, `Failed to parse either. First: Failed to parse either. First: Not a string; Second: Not a list: {}; Second: No matching codec found`},
		{`{type:"minecraft:notice",title:"x",body:{type:"minecraft:item",item:"stone",description:{}}}`, `Failed to parse either. First: No key contents in MapLike[{}]; Second: Failed to parse either. First: Failed to parse either. First: Not a string; Second: Not a list: {}; Second: No matching codec found`},
	} {
		input := "dialog show Bob " + tc.dialog
		alice.ChatCommand(input)
		alice.ExpectCommandError(
			servertest.CommandErrorMessageForTest("argument.resource_or_id.failed_to_parse", tc.msg),
			servertest.CommandErrorContextForTest(input, len(input)),
		)
	}

	for _, ok := range []string{
		`{type:"minecraft:multi_action",title:"x",actions:[{label:"a"}]}`,
		`{type:"minecraft:notice",title:"x",pause:0,after_action:"none"}`,
		`{type:"minecraft:notice",title:"x",body:{type:"minecraft:item",item:"stone"},inputs:[{type:"minecraft:text",key:"a_1",label:"l",max_length:3,initial:"abc"},{type:"minecraft:single_option",key:"o",label:"l",options:["a",{id:"b",initial:1b}]},{type:"minecraft:number_range",key:"n",label:"l",start:1,end:0,initial:0.5f,step:0.1f},{type:"minecraft:boolean",key:"b",label:"l"}]}`,
		`{type:"minecraft:notice",title:"x",inputs:[{type:"minecraft:number_range",key:"n",label:"l",start:0.1f,end:1,initial:0.1d}]}`,
		`{type:"minecraft:notice",title:"x",action:{label:"a",action:{type:"open_url",url:"https://example.com"}}}`,
		`{type:"minecraft:notice",title:"x",action:{label:"a",action:{type:"run_command",command:"say hi"}}}`,
		`{type:"minecraft:notice",title:"x",action:{label:"a",action:{type:"change_page",page:2}}}`,
		`{type:"minecraft:notice",title:"x",action:{label:"a",action:{type:"show_dialog",dialog:"minecraft:server_links"}}}`,
		`{type:"minecraft:notice",title:"x",action:{label:"a",action:{type:"custom",id:"a:b",payload:1}}}`,
		`{type:"minecraft:notice",title:"x",action:{label:"a",action:{type:"dynamic/custom",id:"a:b"}}}`,
		`{type:"minecraft:notice",title:"x",action:{label:"a",action:{type:"dynamic/run_command",template:"say $(k)"}}}`,
		`{type:"minecraft:notice",title:"x",action:{label:"a",action:{type:"copy_to_clipboard",value:"v"}}}`,
		`{type:"minecraft:dialog_list",title:"x",dialogs:"#minecraft:quick_actions"}`,
		`{type:"minecraft:notice",title:"x",action:{label:"a",width:100.0d}}`,
		`{type:"minecraft:notice",title:{text:"x",click_event:{action:"open_url",url:"https://example.com/a?b=c"}},body:{type:"minecraft:item",item:"stone",description:"d",show_tooltip:0b}}`,
	} {
		alice.ChatCommand("dialog show Bob " + ok)
		servertest.AwaitCommandFeedbackText(t, alice, "commands.dialog.show.single", servertest.PlayerDisplayComponent("Bob"))
	}
	alice.Disconnect()
	bob.Disconnect()
}

// textFail is the message a text component the codec refuses carries.
func textFail(compound string) string {
	return "Failed to parse either. First: Failed to parse either. First: Not a string; Second: Not a list: " +
		compound + "; Second: No matching codec found"
}
