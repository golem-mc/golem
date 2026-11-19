package commands

import (
	"testing"

	"github.com/golem-mc/golem/internal/server/servertest"
)

// vanilla: ComponentSerialization.createCodec, Style.Serializer, CommandArgumentParser.withCodec.
// Expectations were read from the 26.3 component codec with registry-aware NbtOps.
func TestComponentArgumentCodecDiagnostics(t *testing.T) {
	t.Parallel()
	_, c, _ := servertest.JoinAs(t, servertest.SurvivalTestConfig(), "DJ", servertest.OpsListing(t, "DJ"))
	c.Settle()
	cases := []struct{ component, message string }{
		{`{text:"x",bold:"yes"}`, `Not a number`},
		{`{text:"x",color:1}`, `Not a string`},
		{`{text:"x",font:1}`, `Not a string`},
		{`{text:"x",font:"BAD"}`, `Not a valid resource location: BAD Non [a-z0-9/._-] character in path of location: minecraft:BAD`},
		{`{text:"x",shadow_color:"x"}`, `Failed to parse either. First: Not a number; Second: Not a list: "x"`},
		{`{text:"x",shadow_color:[1,2]}`, `Failed to parse either. First: Not a number; Second: Input is not a list of 4 elements`},
		{`{text:"x",shadow_color:[1,"x",2,3]}`, `Failed to parse either. First: Not a number; Second: Not a number; Input is not a list of 4 elements`},
		{`{text:"x",insertion:1}`, `Not a string`},
		{`{text:"x",color:"nope",bold:"yes",font:"BAD",extra:[]}`, `Not a valid resource location: BAD Non [a-z0-9/._-] character in path of location: minecraft:BAD; Not a number; Invalid color name: nope; List must have contents`},
		{`{text:1,color:"nope",bold:"yes"}`, `Failed to parse either. First: Failed to parse either. First: Not a string; Second: Not a list: {bold:"yes",color:"nope",text:1}; Second: Not a number; Invalid color name: nope; No matching codec found`},
		{`{text:"x",extra:[5,{}]}`, `Failed to parse either. First: Failed to parse either. First: Not a string; Second: Not a list: {}; Second: No matching codec found; Failed to parse either. First: Failed to parse either. First: Not a string; Second: Not a list: 5; Second: Not a map: 5; List must have contents`},
		{`["ok",5,{}]`, `Failed to parse either. First: Failed to parse either. First: Not a string; Second: Not a list: {}; Second: No matching codec found; Failed to parse either. First: Failed to parse either. First: Not a string; Second: Not a list: 5; Second: Not a map: 5`},
		{`{text:"x",click_event:5}`, `Not a map: 5`},
		{`{text:"x",click_event:{}}`, `No key action in MapLike[{}]`},
		{`{text:"x",click_event:{action:"bad"}}`, `Unknown element name:bad`},
		{`{text:"x",click_event:{action:"open_file",path:"x"}}`, `Click event type not allowed: OPEN_FILE`},
		{`{text:"x",click_event:{action:"open_url"}}`, `No key url in MapLike[{action:"open_url"}]`},
		{`{text:"x",click_event:{action:"open_url",url:"foo"}}`, `Missing protocol in URI: foo: foo`},
		{`{text:"x",click_event:{action:"open_url",url:"ftp://x"}}`, `Unsupported protocol in URI: ftp://x: ftp://x`},
		{`{text:"x",click_event:{action:"change_page",page:0}}`, `Value must be positive: 0`},
		{`{text:"x",hover_event:5}`, `Not a map: 5`},
		{`{text:"x",hover_event:{}}`, `No key action in MapLike[{}]`},
		{`{text:"x",hover_event:{action:"bad"}}`, `Unknown element name:bad`},
		{`{text:"x",hover_event:{action:"show_text"}}`, `No key value in MapLike[{action:"show_text"}]`},
		{`{text:"x",hover_event:{action:"show_text",value:{text:1}}}`, `Failed to parse either. First: Failed to parse either. First: Not a string; Second: Not a list: {text:1}; Second: No matching codec found`},
		{`{text:"x",hover_event:{action:"show_entity"}}`, `No key uuid in MapLike[{action:"show_entity"}]; No key id in MapLike[{action:"show_entity"}]`},
		{`{text:"x",hover_event:{action:"show_item"}}`, `No key id in MapLike[{action:"show_item"}]`},
		{`{type:"bogus"}`, `Failed to parse either. First: Failed to parse either. First: Not a string; Second: Not a list: {type:"bogus"}; Second: Unknown element id: bogus`},
		{`{type:5}`, `Failed to parse either. First: Failed to parse either. First: Not a string; Second: Not a list: {type:5}; Second: Not a string`},
		{`{type:"text",text:5}`, `Failed to parse either. First: Failed to parse either. First: Not a string; Second: Not a list: {text:5,type:"text"}; Second: Not a string`},
		{`{selector:"@bad"}`, `Failed to parse either. First: Failed to parse either. First: Not a string; Second: Not a list: {selector:"@bad"}; Second: No matching codec found`},
		{`{type:"selector",selector:"@bad"}`, `Failed to parse either. First: Failed to parse either. First: Not a string; Second: Not a list: {selector:"@bad",type:"selector"}; Second: Invalid selector component: @bad: Unknown selector type '@b' at position 1: @<--[HERE]`},
		{`[]`, `Failed to parse either. First: Failed to parse either. First: Not a string; Second: List must have contents; Second: Not a map: []`},
		{`[{},5]`, `Failed to parse either. First: Failed to parse either. First: Not a string; Second: Failed to parse either. First: Failed to parse either. First: Not a string; Second: Not a list: 5; Second: Not a map: 5; Failed to parse either. First: Failed to parse either. First: Not a string; Second: Not a list: {}; Second: No matching codec found; List must have contents; Second: Not a map: [{},5]`},
		{`[{text:"x",bold:"bad"}]`, `Not a number`},
		{`{type:"translatable",translate:"x",with:5}`, `Not a list: 5`},
		{`{type:"selector",selector:"@a",separator:5}`, `Failed to parse either. First: Failed to parse either. First: Not a string; Second: Not a list: 5; Second: Not a map: 5`},
		{`{type:"text"}`, `Failed to parse either. First: Failed to parse either. First: Not a string; Second: Not a list: {type:"text"}; Second: No key text in MapLike[{type:"text"}]`},
		{`{type:"nbt",nbt:"x"}`, `Failed to parse either. First: Failed to parse either. First: Not a string; Second: Not a list: {nbt:"x",type:"nbt"}; Second: No matching codec found`},
		{`{type:"score",score:{}}`, `Failed to parse either. First: Failed to parse either. First: Not a string; Second: Not a list: {score:{},type:"score"}; Second: No key objective in MapLike[{}]; No key name in MapLike[{}]`},
		{`{type:"nbt",nbt:"x",storage:"a:b",interpret:true,plain:true}`, `Failed to parse either. First: Failed to parse either. First: Not a string; Second: Not a list: {interpret:1b,nbt:"x",plain:1b,storage:"a:b",type:"nbt"}; Second: 'interpret' and 'plain' flags can't be both on`},
		{`{text:"x",hover_event:{action:"show_item",id:"air"}}`, `Item must not be minecraft:air`},
		{`{text:"x",hover_event:{action:"show_item",id:"bad",count:0,components:5}}`, `Not a map: 5; Value must be within range [1;99]: 0; Unknown registry key in ResourceKey[minecraft:root / minecraft:item]: minecraft:bad`},
		{`{text:"x",hover_event:{action:"show_entity",id:"bad",uuid:"bad",name:5}}`, `Failed to parse either. First: Failed to parse either. First: Not a string; Second: Not a list: 5; Second: Not a map: 5; Failed to parse either. First: Not a list; Second: Invalid UUID bad: Invalid UUID string: bad; Unknown registry key in ResourceKey[minecraft:root / minecraft:entity_type]: minecraft:bad`},
		{`{text:"x",hover_event:{action:"show_entity",id:"zombie",uuid:[I;1,2]}}`, `Failed to parse either. First: Input is not a list of 4 ints; Second: Not a string`},
		{`{text:"x",hover_event:{action:"show_entity",id:"zombie",uuid:5}}`, `Failed to parse either. First: Not a list; Second: Not a string`},
		{`{text:"x",hover_event:{action:"show_item",id:"stone",components:{"bad":1}}}`, `No component with type: 'minecraft:bad'; No component with type: 'minecraft:bad' missed input: {bad:1}`},
		{`{text:"x",hover_event:{action:"show_item",id:"stone",components:{"!damage":5}}}`, `Not a map: 5 missed input: {"!damage":5}`},
		{`{text:"x",click_event:{action:"run_command",command:"\u00a7c"}}`, `Disallowed chat character: '§'`},
		{`{text:"x",click_event:{action:"open_url",url:"http:"}}`, `Expected scheme-specific part at index 5: http:`},
		{`{text:"x",click_event:{action:5}}`, `Not a string`},
		{`{text:"x",hover_event:{action:"show_entity",id:"player",uuid:"32c1e9d0-a046-34fb-8bc1-3986ea745dc40"}}`, `Failed to parse either. First: Not a list; Second: Invalid UUID 32c1e9d0-a046-34fb-8bc1-3986ea745dc40: UUID string too large`},
		{`{text:"x",hover_event:{action:"show_entity",id:"player",uuid:"8000000000000000-0-0-0-0"}}`, `Failed to parse either. First: Not a list; Second: Invalid UUID 8000000000000000-0-0-0-0: Error at index 15 in: "8000000000000000"`},
		{`{text:"x",hover_event:{action:"show_entity",id:"player",uuid:"g-0-0-0-0"}}`, `Failed to parse either. First: Not a list; Second: Invalid UUID g-0-0-0-0: Error at index 0 in: "g"`},
		{`{text:"x",hover_event:{action:"show_entity",id:"player",uuid:"-0-0-0-0"}}`, `Failed to parse either. First: Not a list; Second: Invalid UUID -0-0-0-0: For input string: "" under radix 16`},
		{`{text:"x",click_event:{action:"open_url",url:"http://"}}`, `Expected authority at index 7: http://`},
		{`{text:"x",click_event:{action:"open_url",url:"http://example.com/a b"}}`, `Illegal character in path at index 20: http://example.com/a b`},
		{`{text:"x",click_event:{action:"open_url",url:"http://example.com/{x}"}}`, `Illegal character in path at index 19: http://example.com/{x}`},
		{`{type:"nbt",source:"block",nbt:"x",block:"foo"}`, `Failed to parse either. First: Failed to parse either. First: Not a string; Second: Not a list: {block:"foo",nbt:"x",source:"block",type:"nbt"}; Second: Invalid coordinates path: foo: Expected integer at position 0: <--[HERE]`},
		{`{type:"nbt",source:"block",nbt:"x",block:"0 0"}`, `Failed to parse either. First: Failed to parse either. First: Not a string; Second: Not a list: {block:"0 0",nbt:"x",source:"block",type:"nbt"}; Second: Invalid coordinates path: 0 0: Incomplete (expected 3 coordinates) at position 0: <--[HERE]`},
		{`{type:"nbt",source:"block",nbt:"x",block:"0 0 0x"}`, `Failed to parse either. First: Failed to parse either. First: Not a string; Second: Not a list: {block:"0 0 0x",nbt:"x",source:"block",type:"nbt"}; Second: Invalid coordinates path: 0 0 0x: Trailing data found: 0 0 0[x]`},
		{`{type:"nbt",source:"block",nbt:"x",block:"0 0 ^"}`, `Failed to parse either. First: Failed to parse either. First: Not a string; Second: Not a list: {block:"0 0 ^",nbt:"x",source:"block",type:"nbt"}; Second: Invalid coordinates path: 0 0 ^: Cannot mix world and local coordinates (everything must either use ^ or not) at position 4: 0 0 <--[HERE]`},
		{`{type:"nbt",source:"block",nbt:"x",block:"^ ^ 0"}`, `Failed to parse either. First: Failed to parse either. First: Not a string; Second: Not a list: {block:"^ ^ 0",nbt:"x",source:"block",type:"nbt"}; Second: Invalid coordinates path: ^ ^ 0: Cannot mix world and local coordinates (everything must either use ^ or not) at position 0: <--[HERE]`},
		{`{text:"x",hover_event:{action:"show_item",id:"stone",components:{"creative_slot_lock":{}}}}`, `'minecraft:creative_slot_lock' is not a persistent component; 'minecraft:creative_slot_lock' is not a persistent component missed input: {creative_slot_lock:{}}`},
		{`{text:"x",hover_event:{action:"show_item",id:"stone",components:{"damage":-1}}}`, `Value must be non-negative: -1 missed input: {damage:-1}`},
		{`{text:"x",italic:"yes",underlined:"yes",strikethrough:"yes",obfuscated:"yes"}`, `Not a number; Not a number; Not a number; Not a number`},
		{`{text:"x",extra:5}`, `Not a list: 5`},
		{`{text:"x",extra:[{text:"child",font:"BAD"}]}`, `Not a valid resource location: BAD Non [a-z0-9/._-] character in path of location: minecraft:BAD`},
		{`{type:"object",object:"atlas",sprite:"BAD",fallback:5}`, `Failed to parse either. First: Failed to parse either. First: Not a string; Second: Not a list: {fallback:5,object:"atlas",sprite:"BAD",type:"object"}; Second: Failed to parse either. First: Failed to parse either. First: Not a string; Second: Not a list: 5; Second: Not a map: 5; Not a valid resource location: BAD Non [a-z0-9/._-] character in path of location: minecraft:BAD`},
		{`{type:"object",object:"atlas",sprite:"x",atlas:5}`, `Not a string`},
		{`{type:"object",object:"player",player:"Bob",hat:"yes"}`, `Not a number`},
		{`{type:"object",object:"bad",fallback:5}`, `Failed to parse either. First: Failed to parse either. First: Not a string; Second: Not a list: {fallback:5,object:"bad",type:"object"}; Second: Failed to parse either. First: Failed to parse either. First: Not a string; Second: Not a list: 5; Second: Not a map: 5; Unknown element id: bad`},
		{`{type:"nbt",nbt:"x",source:"storage",storage:"BAD"}`, `Failed to parse either. First: Failed to parse either. First: Not a string; Second: Not a list: {nbt:"x",source:"storage",storage:"BAD",type:"nbt"}; Second: Not a valid resource location: BAD Non [a-z0-9/._-] character in path of location: minecraft:BAD`},
		{`{type:"nbt",nbt:"x",source:"entity",entity:"@bad"}`, `Failed to parse either. First: Failed to parse either. First: Not a string; Second: Not a list: {entity:"@bad",nbt:"x",source:"entity",type:"nbt"}; Second: Invalid selector component: @bad: Unknown selector type '@b' at position 1: @<--[HERE]`},
		{`{type:"nbt",nbt:5,source:5}`, `Failed to parse either. First: Failed to parse either. First: Not a string; Second: Not a list: {nbt:5,source:5,type:"nbt"}; Second: Not a string; Not a string`},
		{`{type:"selector",selector:"@a[limit=0]"}`, `Failed to parse either. First: Failed to parse either. First: Not a string; Second: Not a list: {selector:"@a[limit=0]",type:"selector"}; Second: Invalid selector component: @a[limit=0]: Limit must be at least 1 at position 9: @a[limit=<--[HERE]`},
		{`{type:"selector",selector:"@a junk"}`, `Failed to parse either. First: Failed to parse either. First: Not a string; Second: Not a list: {selector:"@a junk",type:"selector"}; Second: Invalid selector component: @a junk: Trailing data found: @a[ junk]`},
		{`{type:"score",score:{name:5,objective:5}}`, `Failed to parse either. First: Failed to parse either. First: Not a string; Second: Not a list: {score:{name:5,objective:5},type:"score"}; Second: Not a string; Failed to parse either. First: Not a string; Second: Not a string`},
		{`{type:"translatable",translate:"x",with:[{}]}`, `Failed to parse either. First: This value needs to be parsed as component; Second: Failed to parse either. First: Failed to parse either. First: Not a string; Second: Not a list: {}; Second: No matching codec found`},
		{`{type:"translatable",translate:"x",with:[{text:"x",bold:"yes"}]}`, `Not a number`},
		{`{text:"x",hover_event:{action:"show_text",value:{text:"child",font:"BAD"}}}`, `Not a valid resource location: BAD Non [a-z0-9/._-] character in path of location: minecraft:BAD`},
		{`{text:"x",click_event:{action:"copy_to_clipboard",value:1}}`, `Not a string`},
		{`{text:"x",click_event:{action:"custom",id:"BAD"}}`, `Not a valid resource location: BAD Non [a-z0-9/._-] character in path of location: minecraft:BAD`},
		{`[I;1,2]`, `Failed to parse either. First: Failed to parse either. First: Not a string; Second: Failed to parse either. First: Failed to parse either. First: Not a string; Second: Not a list: 2; Second: Not a map: 2; Failed to parse either. First: Failed to parse either. First: Not a string; Second: Not a list: 1; Second: Not a map: 1; List must have contents; Second: Not a map: [I;1,2]`},
		{`{text:"x",click_event:{action:"open_url",url:"http://a#b#c"}}`, `Illegal character in fragment at index 10: http://a#b#c`},
		{`{text:"x",click_event:{action:"open_url",url:"http://a##"}}`, `Illegal character in fragment at index 9: http://a##`},
		{`{text:"x",click_event:{action:"open_url",url:"http:#"}}`, `Expected scheme-specific part at index 5: http:#`},
		{`{text:"x",click_event:{action:"open_url",url:"http:#x"}}`, `Expected scheme-specific part at index 5: http:#x`},
		{`{text:"x",click_event:{action:"open_url",url:"http://[abc]/"}}`, `IPv6 address too short at index 8: http://[abc]/`},
		{`{text:"x",click_event:{action:"open_url",url:"http://a[b]/"}}`, `Illegal character in hostname at index 8: http://a[b]/`},
		{`{text:"x",click_event:{action:"open_url",url:"http://[::1.2.3.256]"}}`, `Malformed IPv4 address at index 16: http://[::1.2.3.256]`},
		{`{text:"x",click_event:{action:"open_url",url:"http://a@@[::1]"}}`, `Illegal character in hostname at index 9: http://a@@[::1]`},
		{`{text:"x",click_event:{action:"open_url",url:"https://a:b@@[::1]:80"}}`, `Illegal character in hostname at index 12: https://a:b@@[::1]:80`},
		{`{text:"x",click_event:{action:"open_url",url:"http://auser@@[::1]"}}`, `Illegal character in hostname at index 13: http://auser@@[::1]`},
		{`{text:"x",click_event:{action:"open_url",url:"http://[12345::]"}}`, `IPv6 hexadecimal digit sequence too long at index 8: http://[12345::]`},
		{`{text:"x",click_event:{action:"open_url",url:"http://[::1]:bad"}}`, `Illegal character in port number at index 13: http://[::1]:bad`},
		{`{text:"x",click_event:{action:"open_url",url:"http://[::1]:999999999999"}}`, `Malformed port number at index 13: http://[::1]:999999999999`},
		{`{text:"x",click_event:{action:"open_url",url:"http://[::1%]"}}`, `scope id expected: http://[::1%]`},
		{`{text:"x",click_event:{action:"open_url",url:"http://[::1%x-y]"}}`, `Illegal character in scope id at index 13: http://[::1%x-y]`},
		{`{text:"x",click_event:{action:"open_url",url:"http://[::"}}`, `Expected closing bracket for IPv6 address at index 10: http://[::`},
		{`{text:"x",click_event:{action:"open_url",url:"http://[]"}}`, `Expected closing bracket for IPv6 address at index 8: http://[]`},
		{`{text:"x",click_event:{action:"open_url",url:"http://[1:2:3]"}}`, `IPv6 address too short at index 8: http://[1:2:3]`},
		{`{text:"x",click_event:{action:"open_url",url:"foo/bar:x"}}`, `Missing protocol in URI: foo/bar:x: foo/bar:x`},
		{`{text:"x",click_event:{action:"open_url",url:"/path#frag:x"}}`, `Missing protocol in URI: /path#frag:x: /path#frag:x`},
		{`{text:"x",click_event:{action:"open_url",url:"http://a/%zz"}}`, `Malformed escape pair at index 9: http://a/%zz`},
		{`{text:"x",click_event:{action:"open_url",url:"http://a?x y"}}`, `Illegal character in query at index 10: http://a?x y`},
		{`{text:"x",click_event:{action:"open_url",url:":foo"}}`, `Expected scheme name at index 0: :foo`},
	}
	for _, prefix := range []string{
		"tellraw @s ", "bossbar add test:invalid ", "bossbar set test:invalid name ", "title @s title ",
		"title @s subtitle ", "title @s actionbar ", "datapack create invalid ",
		"scoreboard objectives add invalid dummy ",
	} {
		t.Run(prefix, func(t *testing.T) {
			for _, tc := range cases {
				t.Run(tc.component, func(t *testing.T) {
					input := prefix + tc.component
					c.ChatCommand(input)
					c.ExpectCommandError(
						servertest.CommandErrorMessageForTest("argument.component.invalid", tc.message),
						servertest.CommandErrorContextForTest(input, len(prefix)),
					)
				})
			}
		})
	}
}
