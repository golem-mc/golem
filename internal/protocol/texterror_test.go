package protocol

import (
	"testing"

	"github.com/golem-mc/golem/nbt"
)

// componentErrorCases are inputs a vanilla 26.3 server refused as a
// scoreboard component, with the message after "Invalid chat component: ".
var componentErrorCases = [][2]string{
	{`5`, `Failed to parse either. First: Failed to parse either. First: Not a string; Second: Not a list: 5; Second: Not a map: 5`},
	{`1b`, `Failed to parse either. First: Failed to parse either. First: Not a string; Second: Not a list: 1b; Second: Not a map: 1b`},
	{`{text:1}`, `Failed to parse either. First: Failed to parse either. First: Not a string; Second: Not a list: {text:1}; Second: No matching codec found`},
	{`{foo:1}`, `Failed to parse either. First: Failed to parse either. First: Not a string; Second: Not a list: {foo:1}; Second: No matching codec found`},
	{`{}`, `Failed to parse either. First: Failed to parse either. First: Not a string; Second: Not a list: {}; Second: No matching codec found`},
	{`[]`, `Failed to parse either. First: Failed to parse either. First: Not a string; Second: List must have contents; Second: Not a map: []`},
	{`[{}]`, `Failed to parse either. First: Failed to parse either. First: Not a string; Second: Failed to parse either. First: Failed to parse either. First: Not a string; Second: Not a list: {}; Second: No matching codec found; List must have contents; Second: Not a map: [{}]`},
	{`[1]`, `Failed to parse either. First: Failed to parse either. First: Not a string; Second: Failed to parse either. First: Failed to parse either. First: Not a string; Second: Not a list: 1; Second: Not a map: 1; List must have contents; Second: Not a map: [1]`},
	{`[1,2]`, `Failed to parse either. First: Failed to parse either. First: Not a string; Second: Failed to parse either. First: Failed to parse either. First: Not a string; Second: Not a list: 2; Second: Not a map: 2; Failed to parse either. First: Failed to parse either. First: Not a string; Second: Not a list: 1; Second: Not a map: 1; List must have contents; Second: Not a map: [1,2]`},
	{`[1,"a"]`, `Failed to parse either. First: Failed to parse either. First: Not a string; Second: Not a list: 1; Second: Not a map: 1`},
	{`["a",1]`, `Failed to parse either. First: Failed to parse either. First: Not a string; Second: Not a list: 1; Second: Not a map: 1`},
	{`[[]]`, `Failed to parse either. First: Failed to parse either. First: Not a string; Second: Failed to parse either. First: Failed to parse either. First: Not a string; Second: List must have contents; Second: Not a map: []; List must have contents; Second: Not a map: [[]]`},
	{`[[1]]`, `Failed to parse either. First: Failed to parse either. First: Not a string; Second: Failed to parse either. First: Failed to parse either. First: Not a string; Second: Failed to parse either. First: Failed to parse either. First: Not a string; Second: Not a list: 1; Second: Not a map: 1; List must have contents; Second: Not a map: [1]; List must have contents; Second: Not a map: [[1]]`},
	{`{text:"x",color:"nope"}`, `Invalid color name: nope`},
	{`{text:"x",bold:"yes"}`, `Not a number`},
	{`{text:"x",extra:[]}`, `List must have contents`},
	{`{text:"x",extra:[1]}`, `Failed to parse either. First: Failed to parse either. First: Not a string; Second: Not a list: 1; Second: Not a map: 1; List must have contents`},
	{`{text:"x",extra:[{text:2}]}`, `Failed to parse either. First: Failed to parse either. First: Not a string; Second: Not a list: {text:2}; Second: No matching codec found; List must have contents`},
	{`{translate:1}`, `Failed to parse either. First: Failed to parse either. First: Not a string; Second: Not a list: {translate:1}; Second: No matching codec found`},
	{`{translate:"a",with:5}`, `Failed to parse either. First: Failed to parse either. First: Not a string; Second: Not a list: {translate:"a",with:5}; Second: No matching codec found`},
	{`{text:"x",font:"BAD FONT"}`, `Not a valid resource location: BAD FONT Non [a-z0-9/._-] character in path of location: minecraft:BAD FONT`},
	{`{text:"x",click_event:{action:"nope"}}`, `Unknown element name:nope`},
	{`{text:"x",click_event:{action:"open_url"}}`, `No key url in MapLike[{action:"open_url"}]`},
	{`{text:"x",hover_event:{action:"nope"}}`, `Unknown element name:nope`},
	{`{text:"x",hover_event:{action:"show_text"}}`, `No key value in MapLike[{action:"show_text"}]`},
	{`{text:"x",hover_event:{action:"show_entity",id:"minecraft:nope",name:"S",uuid:[I;1,2,3,4]}}`, `Unknown registry key in ResourceKey[minecraft:root / minecraft:entity_type]: minecraft:nope`},
	{`{text:"x",hover_event:{action:"show_entity",id:"minecraft:player",uuid:[I;1,2,3]}}`, `Failed to parse either. First: Input is not a list of 4 ints; Second: Not a string`},
	{`{text:"x",shadow_color:"x"}`, `Failed to parse either. First: Not a number; Second: Not a list: "x"`},
	{`{text:"x",type:"nope"}`, `Failed to parse either. First: Failed to parse either. First: Not a string; Second: Not a list: {text:"x",type:"nope"}; Second: Unknown element id: nope`},
	{`{text:"x",color:"nope",bold:"yes"}`, `Not a number; Invalid color name: nope`},
	{`{text:"x",extra:[],color:"nope"}`, `Invalid color name: nope; List must have contents`},
	{`{text:"x",color:1}`, `Not a string`},
	{`{text:"x",color:"#zzzzzz"}`, `Invalid color value: #zzzzzz`},
	{`{text:"x",color:"#1000000"}`, `Color value out of range: #1000000`},
	{`{text:"x",italic:"a"}`, `Not a number`},
	{`{text:"x",insertion:1}`, `Not a string`},
	{`{text:"x",font:1}`, `Not a string`},
	{`{text:"x",click_event:1}`, `Not a map: 1`},
	{`{text:"x",click_event:{}}`, `No key action in MapLike[{}]`},
	{`{text:"x",click_event:{action:1}}`, `Not a string`},
	{`{text:"x",click_event:{action:"open_url",url:1}}`, `Not a string`},
	{`{text:"x",click_event:{action:"open_url",url:"nope"}}`, `Missing protocol in URI: nope: nope`},
	{`{text:"x",click_event:{action:"run_command",command:1}}`, `Not a string`},
	{`{text:"x",click_event:{action:"change_page",page:0}}`, `Value must be positive: 0`},
	{`{text:"x",click_event:{action:"change_page",page:"a"}}`, `Not a number`},
	{`{text:"x",click_event:{action:"open_file",path:"a"}}`, `Click event type not allowed: OPEN_FILE`},
	{`{text:"x",hover_event:1}`, `Not a map: 1`},
	{`{text:"x",hover_event:{}}`, `No key action in MapLike[{}]`},
	{`{text:"x",hover_event:{action:"show_text",value:{text:1}}}`, `Failed to parse either. First: Failed to parse either. First: Not a string; Second: Not a list: {text:1}; Second: No matching codec found`},
	{`{text:"x",hover_event:{action:"show_entity",id:1,name:"S",uuid:[I;1,2,3,4]}}`, `Not a string`},
	{`{text:"x",hover_event:{action:"show_entity",id:"minecraft:player",name:"S"}}`, `No key uuid in MapLike[{action:"show_entity",id:"minecraft:player",name:"S"}]`},
	{`{text:"x",extra:5}`, `Not a list: 5`},
	{`{translate:"a",with:5,color:"nope"}`, `Failed to parse either. First: Failed to parse either. First: Not a string; Second: Not a list: {color:"nope",translate:"a",with:5}; Second: Invalid color name: nope; No matching codec found`},
	{`{text:"x",extra:[{text:"y",color:"nope"}]}`, `Invalid color name: nope`},
	{`{text:"x",extra:["a",{text:"y",color:"nope"}]}`, `Invalid color name: nope`},
	{`{text:"x",extra:[{text:"y",color:"nope"},1]}`, `Failed to parse either. First: Failed to parse either. First: Not a string; Second: Not a list: 1; Second: Not a map: 1; Invalid color name: nope`},
	{`{text:"x",extra:[1,{text:"y",color:"nope"}]}`, `Invalid color name: nope; Failed to parse either. First: Failed to parse either. First: Not a string; Second: Not a list: 1; Second: Not a map: 1`},
	{`[{text:"y",color:"nope"}]`, `Invalid color name: nope`},
	{`[{text:"y",color:"nope"},"a"]`, `Invalid color name: nope`},
	{`{text:"x",hover_event:{action:"show_text",value:{text:"y",color:"nope"}}}`, `Invalid color name: nope`},
	{`{text:"x",hover_event:{action:"show_text",value:5}}`, `Failed to parse either. First: Failed to parse either. First: Not a string; Second: Not a list: 5; Second: Not a map: 5`},
	{`{text:"x",hover_event:{action:"show_entity",id:"minecraft:nope",name:5,uuid:[I;1,2,3]}}`, `Failed to parse either. First: Failed to parse either. First: Not a string; Second: Not a list: 5; Second: Not a map: 5; Failed to parse either. First: Input is not a list of 4 ints; Second: Not a string; Unknown registry key in ResourceKey[minecraft:root / minecraft:entity_type]: minecraft:nope`},
	{`{text:"x",hover_event:{action:"show_entity",id:"minecraft:player",uuid:5}}`, `Failed to parse either. First: Not a list; Second: Not a string`},
	{`{text:"x",hover_event:{action:"show_entity",id:"minecraft:nope",uuid:5}}`, `Failed to parse either. First: Not a list; Second: Not a string; Unknown registry key in ResourceKey[minecraft:root / minecraft:entity_type]: minecraft:nope`},
	{`{text:"x",click_event:{action:"open_url",url:"nope"},hover_event:{action:"nope"}}`, `Unknown element name:nope; Missing protocol in URI: nope: nope`},
	{`{text:"x",click_event:{action:"change_page",page:0},insertion:1}`, `Not a string; Value must be positive: 0`},
	{`{translate:"a",with:["x",{text:1}]}`, `Failed to parse either. First: Failed to parse either. First: Not a string; Second: Not a list: {translate:"a",with:["x",{text:1}]}; Second: No matching codec found`},
	{`{translate:"a",with:[{text:"y",color:"nope"}]}`, `Failed to parse either. First: Failed to parse either. First: Not a string; Second: Not a list: {translate:"a",with:[{color:"nope",text:"y"}]}; Second: No matching codec found`},
	{`{text:"x",shadow_color:[1.0f,2.0f]}`, `Failed to parse either. First: Not a number; Second: Input is not a list of 4 elements`},
	{`{text:"x",font:"a:B"}`, `Not a valid resource location: a:B Non [a-z0-9/._-] character in path of location: a:B`},
	{`{text:"x",font:"A:b"}`, `Not a valid resource location: A:b Non [a-z0-9_.-] character in namespace of identifier: A:b`},
	{`{text:"x",color:"Red"}`, `Invalid color name: Red`},
	{`{text:"x",hover_event:{action:"show_entity",id:"minecraft:player",name:"S",uuid:"32c1e9d0-a046-34fb-8bc1-3986ea745dc40"}}`, `Failed to parse either. First: Not a list; Second: Invalid UUID 32c1e9d0-a046-34fb-8bc1-3986ea745dc40: UUID string too large`},
	{`{text:"x",click_event:{action:"run_command",command:"a§b"}}`, `Disallowed chat character: '§'`},
	{`{text:"x",click_event:{action:"suggest_command"}}`, `No key command in MapLike[{action:"suggest_command"}]`},
	{`{text:"x",click_event:{action:"copy_to_clipboard",value:1}}`, `Not a string`},
}

// TestComponentErrorMatchesVanilla checks the codec diagnostics recorded from
// a vanilla server.
// vanilla: ComponentSerialization.CODEC
func TestComponentErrorMatchesVanilla(t *testing.T) {
	t.Parallel()
	for _, c := range componentErrorCases {
		tag, _, perr := nbt.ParseValueTag(c[0])
		if perr != nil {
			t.Errorf("%s: %v", c[0], perr)
			continue
		}
		if got := ComponentError(tag); got != c[1] {
			t.Errorf("%s:\n got %q\nwant %q", c[0], got, c[1])
		}
	}
}
