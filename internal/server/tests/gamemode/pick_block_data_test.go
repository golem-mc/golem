package gamemode

import (
	"github.com/golem-mc/golem/internal/server/servertest"
	"testing"

	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/internal/server"
	"github.com/golem-mc/golem/nbt"
	"github.com/golem-mc/golem/registry"
)

// creativePicker joins a creative client and returns the server with it in
// the world.
func creativePicker(t *testing.T) (*server.Server, *servertest.Client) {
	t.Helper()
	return servertest.Dial(t, servertest.BlockTestConfig())
}

// filledChest stands a chest at the position and puts a stack of diamonds in
// slot four.
func filledChest(t *testing.T, srv *server.Server, x, y, z int32) int32 {
	t.Helper()
	diamond, ok := registry.ItemID("minecraft:diamond")
	if !ok {
		t.Fatal("the registry has no diamond")
	}
	if !srv.PlaceContainer(x, y, z, "minecraft:chest") {
		t.Fatal("could not place the chest")
	}
	if !srv.SetInventoryAt(x, y, z, 4, protocol.ItemStack{ItemID: diamond, Count: 5}) {
		t.Fatal("could not fill the chest")
	}
	return diamond
}

// TestPickBlockCopiesAChestsContents checks that a ctrl-pick of a chest
// carries what it holds as the minecraft:container component.
// vanilla: BaseContainerBlockEntity.collectImplicitComponents
func TestPickBlockCopiesAChestsContents(t *testing.T) {
	t.Parallel()
	srv, c := creativePicker(t)
	c.Join("Picker")

	diamond := filledChest(t, srv, 3, 64, 0)
	chestItem, ok := registry.ItemID("minecraft:chest")
	if !ok {
		t.Fatal("the registry has no chest item")
	}
	picked := servertest.PickedStack(t, c, 3, 64, 0, true)
	if picked.ItemID != chestItem {
		t.Errorf("the picked block's item = %d, want the chest %d", picked.ItemID, chestItem)
	}
	if len(picked.Container) != 5 || picked.Container[4].ItemID != diamond || picked.Container[4].Count != 5 {
		t.Errorf("the picked chest's container = %+v, want a diamond stack in slot four", picked.Container)
	}
	if _, _, ok := picked.BlockEntityDataOf(); ok {
		t.Errorf("the picked chest carried block_entity_data besides its container")
	}
	c.Disconnect()
}

// TestPickBlockCopiesBlockEntityComponents checks that a ctrl-pick turns the
// keys a block entity collects into components rather than leaving them in
// minecraft:block_entity_data: a banner's patterns, a skull's profile and
// note block sound, a beehive's bees and a decorated pot's sherds.
// vanilla: BlockEntity.collectImplicitComponents
func TestPickBlockCopiesBlockEntityComponents(t *testing.T) {
	t.Parallel()
	bannerPattern, ok := registry.Get("minecraft:banner_pattern")
	if !ok {
		t.Fatal("the registry has no banner_pattern")
	}
	stripe := bannerPattern.ID("minecraft:stripe_bottom")
	if stripe < 0 {
		t.Fatal("the banner_pattern registry has no stripe_bottom")
	}
	angler, ok := registry.ItemID("minecraft:angler_pottery_sherd")
	if !ok {
		t.Fatal("the registry has no angler pottery sherd")
	}
	bee, ok := registry.EntityTypeID("minecraft:bee")
	if !ok {
		t.Fatal("the registry has no bee")
	}

	cases := []struct {
		name  string
		set   string
		check func(t *testing.T, picked protocol.ItemStack)
	}{
		{"banner", `setblock 3 64 0 minecraft:white_banner{patterns:[{pattern:"minecraft:stripe_bottom",color:"red"}]}`,
			func(t *testing.T, picked protocol.ItemStack) {
				layers, ok := picked.BannerPatternsOf()
				if !ok || len(layers) != 1 {
					t.Fatalf("the picked banner's patterns = %v, %v; want one layer", layers, ok)
				}
				if layers[0].Pattern != stripe || layers[0].Dye != 14 {
					t.Errorf("the picked banner's layer = %+v, want stripe_bottom in red", layers[0])
				}
			}},
		{"skull", `setblock 3 64 0 minecraft:skeleton_skull{profile:{name:"Notch",id:[I;1,2,3,4],properties:[{name:"textures",value:"abc"}]},note_block_sound:"minecraft:bell"}`,
			func(t *testing.T, picked protocol.ItemStack) {
				profile, ok := picked.ProfileOf()
				if !ok {
					t.Fatalf("the picked skull carried no profile: %+v", picked)
				}
				if !profile.HasName || profile.Name != "Notch" || !profile.HasUUID {
					t.Errorf("the picked skull's profile = %+v, want Notch with a UUID", profile)
				}
				if len(profile.Properties) != 1 || profile.Properties[0].Name != "textures" ||
					profile.Properties[0].Value != "abc" {
					t.Errorf("the picked skull's properties = %+v, want one textures property", profile.Properties)
				}
				if sound, ok := picked.NoteBlockSoundOf(); !ok || sound != "minecraft:bell" {
					t.Errorf("the picked skull's note block sound = %q, %v; want bell", sound, ok)
				}
			}},
		{"skull_string_profile", `setblock 3 64 0 minecraft:skeleton_skull{profile:"Notch"}`,
			func(t *testing.T, picked protocol.ItemStack) {
				profile, ok := picked.ProfileOf()
				if !ok {
					t.Fatalf("the picked skull carried no profile: %+v", picked)
				}
				if !profile.HasName || profile.Name != "Notch" || profile.HasUUID {
					t.Errorf("the picked skull's profile = %+v, want the name Notch alone", profile)
				}
			}},
		{"skull_skin", `setblock 3 64 0 minecraft:player_head{profile:{name:"Notch",texture:"minecraft:foo",model:"slim"}}`,
			func(t *testing.T, picked protocol.ItemStack) {
				profile, ok := picked.ProfileOf()
				if !ok {
					t.Fatalf("the picked skull carried no profile: %+v", picked)
				}
				if profile.Skin.Body != "minecraft:foo" || profile.Skin.Model != "slim" {
					t.Errorf("the picked skull's skin = %+v, want texture minecraft:foo and the slim model", profile.Skin)
				}
			}},
		{"beehive", `setblock 3 64 0 minecraft:beehive{bees:[{entity_data:{id:"minecraft:bee"},ticks_in_hive:5,min_ticks_in_hive:600}]}`,
			func(t *testing.T, picked protocol.ItemStack) {
				bees, ok := picked.BeesOf()
				if !ok || len(bees) != 1 {
					t.Fatalf("the picked beehive's bees = %v, %v; want one bee", bees, ok)
				}
				if bees[0].EntityType != bee || bees[0].TicksInHive != 5 || bees[0].MinTicksInHive != 600 {
					t.Errorf("the picked beehive's bee = %+v, want a bee five of six hundred ticks in", bees[0])
				}
				if _, ok := nbt.Get[nbt.String](bees[0].Data.Entries(), "id"); ok {
					t.Errorf("the picked beehive's bee kept its id: %v", bees[0].Data)
				}
			}},
		{"decorated_pot", `setblock 3 64 0 minecraft:decorated_pot{sherds:{back:{id:"minecraft:angler_pottery_sherd"}}}`,
			func(t *testing.T, picked protocol.ItemStack) {
				items, ok := picked.PotDecorationsOf()
				if !ok {
					t.Fatalf("the picked pot carried no pot_decorations: %+v", picked)
				}
				if items[0].ItemID != angler || !items[1].Empty() || !items[2].Empty() || !items[3].Empty() {
					t.Errorf("the picked pot's sherds = %v, want angler back and bare sides elsewhere", items)
				}
			}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			srv, c := servertest.Join(t, servertest.BlockTestConfig(), "Picker", servertest.OpsListing(t, "Picker"))
			// A hive's tick moves its bees' clocks, and the pick copies them
			// as they stand: frozen, they stay what the setblock wrote.
			// vanilla: BeehiveBlockEntity.collectImplicitComponents
			srv.SetTickFrozen(true)
			c.ChatCommand(tc.set)
			c.Settle()

			picked := servertest.PickedStack(t, c, 3, 64, 0, true)
			tc.check(t, picked)
			if _, _, ok := picked.BlockEntityDataOf(); ok {
				t.Errorf("the picked %s left its components in block_entity_data: %+v", tc.name, picked)
			}
			c.Disconnect()
		})
	}
}

// TestPlacingAPickedBlockEntityComponent checks that placing a ctrl-picked
// banner, player head or beehive lays the components its item carries onto
// the raw block entity the placement stands up: a banner's patterns and name,
// a head's profile, note block sound and name, and a beehive's bees.
// vanilla: BannerBlockEntity, SkullBlockEntity, BeehiveBlockEntity
// applyImplicitComponents
func TestPlacingAPickedBlockEntityComponent(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name string
		set  string
		// broadcast is whether placement itself tells the watchers the
		// entity; a beehive has no update packet, so its data reaches
		// watchers with the chunk alone.
		broadcast bool
		check     func(t *testing.T, tag nbt.Compound)
	}{
		{
			name:      "banner",
			broadcast: true,
			set:       `setblock 3 64 0 minecraft:white_banner{patterns:[{pattern:"minecraft:stripe_top",color:"red"}],CustomName:"Flag"}`,
			check: func(t *testing.T, tag nbt.Compound) {
				t.Helper()
				if got, ok := nbt.Get[nbt.String](tag, "CustomName"); !ok || string(got) != "Flag" {
					t.Errorf("the placed banner's CustomName = %q, %v; want Flag", got, ok)
				}
				patterns, ok := nbt.Get[nbt.List](tag, "patterns")
				if !ok || len(patterns.Items) != 1 {
					t.Fatalf("the placed banner's patterns = %v, %v; want one", patterns.Items, ok)
				}
				layer, ok := patterns.Items[0].(nbt.Compound)
				if !ok {
					t.Fatalf("the placed banner's layer = %T, want a compound", patterns.Items[0])
				}
				pattern, _ := nbt.Get[nbt.String](layer, "pattern")
				color, _ := nbt.Get[nbt.String](layer, "color")
				if string(pattern) != "minecraft:stripe_top" || string(color) != "red" {
					t.Errorf("the placed banner's layer = %q %q, want stripe_top red", pattern, color)
				}
			},
		},
		{
			name:      "player head",
			broadcast: true,
			set: `setblock 3 64 0 minecraft:player_head{profile:{name:"Notch",id:[I;1,2,3,4]},` +
				`note_block_sound:"minecraft:block.note_block.pling",custom_name:"Head"}`,
			check: func(t *testing.T, tag nbt.Compound) {
				t.Helper()
				profile, ok := nbt.Get[nbt.Compound](tag, "profile")
				if !ok {
					t.Fatalf("the placed head has no profile: %v", tag)
				}
				if name, _ := nbt.Get[nbt.String](profile, "name"); string(name) != "Notch" {
					t.Errorf("the placed head's profile name = %q, want Notch", name)
				}
				if ids, _ := nbt.Get[nbt.IntArray](profile, "id"); len(ids) != 4 {
					t.Errorf("the placed head's profile id = %v, want four ints", ids)
				}
				if sound, ok := nbt.Get[nbt.String](tag, "note_block_sound"); !ok ||
					string(sound) != "minecraft:block.note_block.pling" {
					t.Errorf("the placed head's note_block_sound = %q, %v", sound, ok)
				}
				if name, ok := nbt.Get[nbt.String](tag, "custom_name"); !ok || string(name) != "Head" {
					t.Errorf("the placed head's custom_name = %q, %v; want Head", name, ok)
				}
			},
		},
		{
			name: "beehive",
			set: `setblock 3 64 0 minecraft:beehive{bees:[{entity_data:{id:"minecraft:bee"},` +
				`ticks_in_hive:5,min_ticks_in_hive:10}]}`,
			check: func(t *testing.T, tag nbt.Compound) {
				t.Helper()
				bees, ok := nbt.Get[nbt.List](tag, "bees")
				if !ok || len(bees.Items) != 1 {
					t.Fatalf("the placed beehive's bees = %v, %v; want one", bees.Items, ok)
				}
				entry, _ := bees.Items[0].(nbt.Compound)
				data, _ := nbt.Get[nbt.Compound](entry, "entity_data")
				if id, _ := nbt.Get[nbt.String](data, "id"); string(id) != "minecraft:bee" {
					t.Errorf("the placed beehive's bee id = %q, want a bee", id)
				}
				if ticks, _ := nbt.IntValue(entry["ticks_in_hive"]); ticks != 5 {
					t.Errorf("the placed beehive's ticks_in_hive = %d, want 5", ticks)
				}
				if min, _ := nbt.IntValue(entry["min_ticks_in_hive"]); min != 10 {
					t.Errorf("the placed beehive's min_ticks_in_hive = %d, want 10", min)
				}
				if _, has := entry["min_ticks_in_hive"]; !has {
					t.Errorf("the placed beehive dropped min_ticks_in_hive: %v", entry)
				}
			},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			srv, c := servertest.Join(t, servertest.BlockTestConfig(), "Picker", servertest.OpsListing(t, "Picker"))
			// Frozen, a hive's bees keep the clocks the setblock wrote through
			// the pick and the placement.
			srv.SetTickFrozen(true)

			c.ChatCommand(tc.set)
			c.Settle()
			servertest.PickedStack(t, c, 3, 64, 0, true)
			c.Settle()

			// The watchers, the placer among them, are told the entity as
			// its placement stands it up, so the patterns and the skin show
			// without waiting for the chunk. vanilla: ChunkHolder.broadcastChanges
			c.PlaceBlock(5, 63, 0, protocol.FaceTop)
			update := false
			c.SettleAndCollect(func(id int32, data []byte) {
				if id != protocol.IDPlayBlockEntityData {
					return
				}
				r := protocol.NewReader(data)
				if x, y, z := r.ReadPosition(); x == 5 && y == 64 && z == 0 {
					update = true
				}
			})
			if update != tc.broadcast {
				t.Errorf("the placed %s sent a block entity data packet = %v, want %v",
					tc.name, update, tc.broadcast)
			}

			tag, ok := srv.BlockEntityTagAt(5, 64, 0)
			if !ok {
				t.Fatalf("the placed %s carries no block entity tag", tc.name)
			}
			tc.check(t, tag)
			c.Disconnect()
		})
	}
}
