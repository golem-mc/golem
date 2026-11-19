package plugins_test

import (
	"encoding/json"
	"reflect"
	"testing"

	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/internal/server/servertest"
	"github.com/golem-mc/golem/nbt"
	"github.com/golem-mc/golem/plugin"
	"github.com/golem-mc/golem/registry"
)

// vanilla: ClickEvent.Custom.CODEC, HoverEvent.ShowItem.CODEC
func TestPluginTextPreservesNBTOnWire(t *testing.T) {
	payload := nbt.Compound{"byte": nbt.Byte(1), "short": nbt.Short(2), "long": nbt.Long(3), "float": nbt.Float(0.5), "bytes": nbt.ByteArray{0, 255}, "longs": nbt.LongArray{4}}
	data, err := nbt.EncodeNetwork(payload)
	if err != nil {
		t.Fatal(err)
	}
	text := plugin.Plain("parent").Bold().Append(plugin.Plain("child"))
	text.Children[0].Style.Bold = new(false)
	text.Style.Click = &plugin.Click{Action: plugin.Custom, Value: "test:click", Payload: data}
	text.Style.Hover = &plugin.Hover{Item: &plugin.ItemStack{Item: registry.ItemDiamond, Count: 2, Patch: plugin.ItemPatch{Other: map[plugin.Key]string{plugin.MinecraftKey("custom_data"): nbt.Stringify(payload)}}}}
	var host plugin.Server
	srv := servertest.NewTestServerWithPlugins(t, servertest.BlockTestConfig(), []plugin.Registration{{Name: "text", Plugin: &testPlugin{enable: func(s plugin.Server) error { host = s; s.SetMOTD(text); return nil }}}})
	roundTrip, _, err := nbt.DecodeNetwork(host.MOTD().Style.Click.Payload)
	if err != nil || !nbt.Equal(roundTrip, payload) {
		t.Fatalf("API click payload = %v, %v", roundTrip, err)
	}
	status := servertest.DialServer(t, srv)
	status.Handshake(protocol.NextStateStatus)
	status.Send(protocol.IDStatusRequest, nil)
	var response struct {
		Description map[string]any `json:"description"`
	}
	if err := json.Unmarshal([]byte(protocol.NewReader(status.Expect(protocol.IDStatusResponse)).ReadString(32767)), &response); err != nil {
		t.Fatal(err)
	}
	event := response.Description["click_event"].(map[string]any)
	jsonPayload := event["payload"].(map[string]any)
	if !reflect.DeepEqual(jsonPayload["bytes"], []any{float64(0), float64(-1)}) {
		t.Errorf("status byte array = %v", jsonPayload["bytes"])
	}
	c := servertest.DialServer(t, srv)
	c.Login("Text")
	c.Configure()
	body := c.ExpectWithinTicks(srv, 0, protocol.IDPlayServerData)
	tag, _, err := nbt.DecodeNetwork(body)
	if err != nil {
		t.Fatal(err)
	}
	root, ok := nbt.CompoundView(tag)
	if !ok {
		t.Fatalf("component = %v", tag)
	}
	click, _ := nbt.CompoundView(root["click_event"])
	if !nbt.Equal(click["payload"], payload) {
		t.Errorf("custom payload = %v, want %v", click["payload"], payload)
	}
	hover, _ := nbt.CompoundView(root["hover_event"])
	components, _ := nbt.CompoundView(hover["components"])
	if !nbt.Equal(components["minecraft:custom_data"], payload) {
		t.Errorf("hover data = %v, want %v", components["minecraft:custom_data"], payload)
	}
	extra, _ := root["extra"].(nbt.List)
	if len(extra.Items) != 1 {
		t.Fatalf("extra = %v", extra)
	}
	child, _ := nbt.CompoundView(extra.Items[0])
	if root["bold"] != nbt.Byte(1) || child["bold"] != nbt.Byte(0) {
		t.Errorf("inheritance overrides lost: %v", tag)
	}
}

// vanilla: Style.Serializer.MAP_CODEC, ClickEvent.CODEC, HoverEvent.CODEC
func TestPluginTextRoundTripsEveryAPIField(t *testing.T) {
	var host plugin.Server
	srv := servertest.NewTestServerWithPlugins(t, servertest.BlockTestConfig(), []plugin.Registration{{Name: "roundtrip", Plugin: &testPlugin{enable: func(s plugin.Server) error { host = s; return nil }}}})
	servertest.HoldTicks(t, srv)
	name := plugin.Plain("Name").NotItalic()
	key := plugin.MinecraftKey("diamond")
	rarity := registry.Rarity(2)
	identity := plugin.UUID{1, 2, 3}
	enchant, ok := registry.EnchantmentID("minecraft:sharpness")
	if !ok {
		t.Fatal("missing sharpness")
	}
	stack := plugin.ItemStack{Item: registry.ItemDiamondSword, Count: 3, Patch: plugin.ItemPatch{
		CustomName: &name, ItemName: &name, Lore: []plugin.Text{name},
		Damage: new(2), MaxDamage: new(200), MaxStackSize: new(4), Unbreakable: new(false),
		Enchantments:       map[plugin.Enchantment]int{plugin.Enchantment(enchant): 3},
		StoredEnchantments: map[plugin.Enchantment]int{plugin.Enchantment(enchant): 4},
		GlintOverride:      new(false), Model: &key,
		CustomModelData: &plugin.CustomModelData{Floats: []float32{1.25}, Flags: []bool{true, false}, Strings: []string{"model"}, Colors: []int32{123}},
		Rarity:          &rarity, HideTooltip: new(false), DyedColor: new(int32(123)),
		Profile: &plugin.Profile{ID: identity, Name: "Tester", Properties: []plugin.ProfileProperty{{Name: "textures", Value: "value", Signature: "signature"}, {Name: "unsigned", Value: "value"}}},
		Data:    map[plugin.Key]any{plugin.MinecraftKey("data"): map[string]any{"byte": int8(2), "short": int16(3), "int": int32(4), "long": int64(5), "float": float32(0.5), "double": float64(1.25), "list": []any{"x", int32(6)}, "bytes": []byte{0, 255}, "ints": []int32{7}, "longs": []int64{8}}},
		Other:   map[plugin.Key]string{plugin.MinecraftKey("repair_cost"): "5", plugin.MinecraftKey("enchantable"): ""},
	}}
	base := plugin.Translate("test.translation", plugin.Text{Keybind: "key.jump"}, plugin.Plain("argument")).Append(plugin.Plain("child"))
	base.Fallback = "Fallback %s %s"
	base.Style = plugin.Style{Color: "#123456", Shadow: new(uint32(0)), Bold: new(false), Italic: new(false), Underlined: new(false), Strikethrough: new(false), Obfuscated: new(false), Font: plugin.MinecraftKey("uniform"), Insertion: "insert"}
	on := base
	on.Style = plugin.Style{Shadow: new(uint32(0xff123456)), Bold: new(true), Italic: new(true), Underlined: new(true), Strikethrough: new(true), Obfuscated: new(true)}
	cases := []plugin.Text{base, on, plugin.Plain(""), plugin.Plain("true").Bold().Italic().Underlined(), plugin.Text{Keybind: "key.sneak"}}
	for _, hover := range []*plugin.Hover{
		{Text: &base}, {Item: &stack}, {Entity: &plugin.HoverEntity{Type: registry.EntityTypePig, UUID: identity, Name: &name}},
		{Entity: &plugin.HoverEntity{Type: registry.EntityTypePig, UUID: identity}},
	} {
		text := base
		text.Style.Hover = hover
		cases = append(cases, text)
	}
	for i, value := range []string{"https://example.com", "/help", "/say hello", "2", "", "minecraft:custom_options", "test:click"} {
		text := base
		text.Style.Click = &plugin.Click{Action: plugin.ClickAction(i), Value: value}
		cases = append(cases, text)
	}
	for i, text := range cases {
		host.SetMOTD(text)
		got := host.MOTD()
		if !reflect.DeepEqual(got, text) {
			t.Errorf("case %d round trip:\n got %#v\nwant %#v", i, got, text)
		}
	}
	// The protocol snapshot owns both the input and output data.
	text := plugin.Plain("owned")
	text.Style.Hover = &plugin.Hover{Item: &stack}
	host.SetMOTD(text)
	stack.Patch.CustomModelData.Floats[0] = 9
	got := host.MOTD()
	got.Style.Hover.Item.Patch.CustomModelData.Floats[0] = 8
	if host.MOTD().Style.Hover.Item.Patch.CustomModelData.Floats[0] != 1.25 {
		t.Fatal("MOTD aliases mutable item data")
	}
}
