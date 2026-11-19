package blocks

import (
	"testing"

	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/internal/server/player"
	"github.com/golem-mc/golem/internal/server/servertest"
	"github.com/golem-mc/golem/nbt"
	"github.com/golem-mc/golem/registry"
)

// The minecraft:block_state data component (data component 76): the block
// state properties a block item carries and applies to the block it places.
// A light picked with level five places the level five state, and a property
// the placed block does not carry is passed by.

// blockStateValue builds the value a stack's minecraft:block_state component
// carries on the wire: a prefixed map, the count then that many pairs of
// property names and values as strings.
// vanilla: BlockItemStateProperties.STREAM_CODEC
func blockStateValue(t *testing.T, props ...[2]string) []byte {
	t.Helper()
	var value protocol.Buffer
	value.PutVarInt(int32(len(props)))
	for _, kv := range props {
		value.PutString(kv[0])
		value.PutString(kv[1])
	}
	return value.Bytes()
}

// itemWithBlockState is a single stack of the named item carrying
// minecraft:block_state with the given properties, the stack a client sends
// for a picked or given block.
func itemWithBlockState(t *testing.T, name string, props ...[2]string) protocol.ItemStack {
	t.Helper()
	id, ok := registry.ItemID(name)
	if !ok {
		t.Fatalf("the registry has no %s", name)
	}
	return protocol.ItemStack{ItemID: id, Count: 1,
		Raw: []protocol.RawComponent{
			{Type: protocol.DataComponentBlockState, Data: blockStateValue(t, props...)},
		}}
}

// lightWithBlockState is a light stack carrying minecraft:block_state with
// the given properties, the stack a client sends for a picked or given light.
func lightWithBlockState(t *testing.T, props ...[2]string) protocol.ItemStack {
	t.Helper()
	return itemWithBlockState(t, "minecraft:light", props...)
}

// lightState is the light block's state at a level, standing dry.
func lightState(t *testing.T, level string) uint16 {
	t.Helper()
	state, ok := registry.FindState("minecraft:light", map[string]string{"level": level})
	if !ok {
		t.Fatalf("the registry has no light at level %s", level)
	}
	return state
}

// TestPlacingAppliesTheBlockStateComponent checks the placement's side of
// the component: a light whose stack carries level five places the level
// five state, both in the world and on the wire, a property the light does
// not carry is passed by, and so is a value the level does not accept.
// vanilla: BlockItem.updateBlockStateFromTag
func TestPlacingAppliesTheBlockStateComponent(t *testing.T) {
	t.Parallel()
	srv, c := servertest.Join(t, servertest.BlockTestConfig(), "Placer")

	// The stack a creative client picks: a light carrying level five.
	c.SetCreativeSlot(player.HotbarSlot, lightWithBlockState(t, [2]string{"level", "5"}))
	c.Settle()
	c.PlaceBlock(5, 63, 0, protocol.FaceTop)
	if got := c.ExpectBlockChange(); got.X != 5 || got.Y != 64 || got.Z != 0 ||
		got.State != lightState(t, "5") {
		t.Errorf("the placement's block update = %+v, want the light at level five", got)
	}
	if got := srv.BlockAt(5, 64, 0); got != lightState(t, "5") {
		t.Errorf("block after place = %d, want the level five light", got)
	}

	// A property the light does not carry is passed by: the level applies,
	// the snow does not.
	c.SetCreativeSlot(player.HotbarSlot,
		lightWithBlockState(t, [2]string{"level", "5"}, [2]string{"snowy", "true"}))
	c.Settle()
	c.PlaceBlock(4, 63, 0, protocol.FaceTop)
	c.ExpectBlockAck()
	if got := srv.BlockAt(4, 64, 0); got != lightState(t, "5") {
		t.Errorf("block after place = %d, want the level five light, the snow passed by", got)
	}

	// A value the level does not accept is passed by too: the light comes
	// in at the level fifteen a plain placement gives.
	c.SetCreativeSlot(player.HotbarSlot, lightWithBlockState(t, [2]string{"level", "16"}))
	c.Settle()
	c.PlaceBlock(3, 63, 0, protocol.FaceTop)
	c.Settle()
	if got := srv.BlockAt(3, 64, 0); got != lightState(t, "15") {
		t.Errorf("block after place = %d, want the level sixteen passed by as fifteen", got)
	}
	c.Disconnect()
}

// TestGivingABlockStatePlacesTheStateItNames checks the /give side of the
// component: vanilla's component syntax names the properties, the stack
// lands in the inventory carrying them, and placing it puts the state they
// name in the world.
func TestGivingABlockStatePlacesTheStateItNames(t *testing.T) {
	t.Parallel()
	cfg := servertest.BlockTestConfig()
	srv, connect := servertest.Start(t, cfg, servertest.WriteOpsFile(t,
		servertest.OpRecord{UUID: protocol.OfflineUUID("Testificate").String(), Name: "Testificate", Level: 2}))
	c := connect()
	c.Login("Testificate")
	c.Configure()
	c.Inventory()
	c.EnterWorld("Testificate")

	// Vanilla's syntax: the item, then its components in brackets.
	c.ChatCommand(`give @s light[block_state={level:"5"}]`)
	updates, chats := servertest.AwaitSlotUpdates(t, c, 1)
	given := lightWithBlockState(t, [2]string{"level", "5"})
	if updates[0].Slot != player.HotbarSlot || !updates[0].Stack.Equal(given) {
		t.Errorf("give slot update = %+v, want the light carrying level five", updates[0])
	}

	// The feedback names the light with the component in its hover, the
	// show-item form of the stack as the files keep it.
	one := int64(1)
	servertest.ExpectFeedbackSoon(t, c, chats, "commands.give.success.single",
		protocol.Text{Number: &one},
		protocol.Text{
			Translate: "chat.square_brackets",
			Color:     "light_purple",
			With:      []protocol.Text{{Extra: []protocol.Text{{Translate: "block.minecraft.light"}}}},
			HoverItem: &protocol.HoverItem{ID: "minecraft:light", Count: 1,
				Components: nbt.Compound{"minecraft:block_state": nbt.Compound{
					"level": nbt.String("5"),
				}}},
		},
		servertest.PlayerDisplayComponent("Testificate"))

	// And the given stack places the state its component names. The settle
	// waits past the placement's own packets, so the world has the state
	// before it is read.
	c.PlaceBlock(5, 63, 0, protocol.FaceTop)
	c.Settle()
	if got, want := srv.BlockAt(5, 64, 0), lightState(t, "5"); got != want {
		t.Errorf("block after place = %d, want the level five light %d", got, want)
	}
	c.Disconnect()
}

// TestPickBlockCarriesTheBlockStateComponent checks the pick-block path: a
// middle-clicked light conjures a light stack whose block_state names its
// level, the component LightBlock.getCloneItemStack always sets, a bee
// nest's honey level travels only when the client asked for block data, and
// a survival pick ignores the flag and swaps the plain carried stack.
// vanilla: LightBlock.getCloneItemStack, BeehiveBlock.getCloneItemStack
func TestPickBlockCarriesTheBlockStateComponent(t *testing.T) {
	t.Parallel()
	srv, c := servertest.Join(t, servertest.BlockTestConfig(), "Picker")

	if !srv.SetBlock(3, 64, 0, lightState(t, "5")) {
		t.Fatal("could not set the light")
	}
	nest, ok := registry.FindState("minecraft:bee_nest", map[string]string{
		"facing": "north", "honey_level": "3",
	})
	if !ok {
		t.Fatal("the registry has no bee nest")
	}
	if !srv.SetBlock(3, 64, 2, nest) {
		t.Fatal("could not set the bee nest")
	}

	// A plain middle-click asks for no block data, and the light carries its
	// level all the same.
	light := servertest.PickedStack(t, c, 3, 64, 0, false)
	if props, ok := light.BlockStatePropertiesOf(); !ok || props["level"] != "5" {
		t.Errorf("the picked light's block_state = %v, %v; want level five", props, ok)
	}

	// The bee nest's honey travels only when the client asked for data.
	withoutData := servertest.PickedStack(t, c, 3, 64, 2, false)
	if _, ok := withoutData.BlockStatePropertiesOf(); ok {
		t.Errorf("the picked bee nest carried a block_state with no data asked for")
	}
	withData := servertest.PickedStack(t, c, 3, 64, 2, true)
	if props, ok := withData.BlockStatePropertiesOf(); !ok || props["honey_level"] != "3" {
		t.Errorf("the picked bee nest's block_state = %v, %v; want honey three", props, ok)
	}
	c.Disconnect()

	// In survival the include-data flag is ignored: the plain bee nest the
	// player carries matches the pick and swaps into the hand, where a
	// carried block_state would have stopped the match and picked nothing.
	plain, ok := registry.ItemID("minecraft:bee_nest")
	if !ok {
		t.Fatal("the registry has no bee nest")
	}
	srvSurvival, survival := servertest.Join(t, servertest.SurvivalTestConfig(), "Picker")
	if !srvSurvival.SetBlock(3, 64, 2, nest) {
		t.Fatal("could not set the bee nest")
	}
	srvSurvival.GiveSlot("Picker", player.MainSlot, protocol.ItemStack{ItemID: plain, Count: 1})
	if got := servertest.PickedStack(t, survival, 3, 64, 2, true); !got.Equal(protocol.ItemStack{ItemID: plain, Count: 1}) {
		t.Errorf("the survival pick of the bee nest = %+v, want the plain bee nest swapped into the hand", got)
	}
	survival.Disconnect()
}
