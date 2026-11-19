package commands

import (
	"fmt"
	"strings"
	"testing"

	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/internal/server/servertest"
	"github.com/golem-mc/golem/registry"
)

func TestRecipeGiveUnlocksTransmuteDyeAndImbueRecipes(t *testing.T) {
	_, c := servertest.Join(t, servertest.SurvivalTestConfig(), "Tester", servertest.OpsListing(t, "Tester"))
	c.WatchBook = true
	for _, tc := range []struct {
		name          string
		entries, kind int32
	}{
		{"black_bundle", 1, 0}, {"blue_shulker_box", 1, 0},
		// vanilla: ServerRecipeBook.addRecipes counts all eight map displays.
		{"map_cloning", 8, 0},
		{"leather_boots_dyed", 1, 0}, {"tipped_arrow", 1, 1},
	} {
		c.Run(t, tc.name, func(t *testing.T) {
			got := runRecipe(c, "recipe give @s minecraft:"+tc.name)
			servertest.CheckCommandFeedback(t, got.chats[0], "commands.recipe.give.success.single", servertest.NumberText(int64(tc.entries)), servertest.PlayerDisplayComponent("Tester"))
			if len(got.adds) != 1 {
				t.Fatalf("got %d add packets, want 1", len(got.adds))
			}
			r := protocol.NewReader(got.adds[0])
			if n := r.ReadVarInt(); n != tc.entries {
				t.Fatalf("entries=%d, want %d", n, tc.entries)
			}
			var ids []int32
			for i := int32(0); i < tc.entries; i++ {
				ids = append(ids, r.ReadVarInt())
				if kind := r.ReadVarInt(); kind != tc.kind {
					t.Fatalf("display type=%d, want %d", kind, tc.kind)
				}
				if tc.kind == 1 {
					if w, h := r.ReadVarInt(), r.ReadVarInt(); w != 3 || h != 3 {
						t.Fatalf("shape=%dx%d", w, h)
					}
				}
				var squares []string
				for range r.ReadVarInt() {
					squares = append(squares, readDynamicSlot(t, r))
				}
				result := readDynamicSlot(t, r)
				if station := readDynamicSlot(t, r); station != fmt.Sprintf("item:%d", registry.ItemCraftingTable) {
					t.Errorf("station=%s", station)
				}
				r.ReadVarInt() // group
				category := r.ReadVarInt()
				if tc.name == "black_bundle" && category != registry.CraftingDisplayOfName("minecraft:bundle").Category {
					t.Errorf("bundle category=%d", category)
				}
				if !r.ReadBool() {
					t.Fatal("missing placement requirements")
				}
				var requirements []string
				for range r.ReadVarInt() {
					requirements = append(requirements, readDynamicSet(r))
				}
				if flags := r.ReadU8(); flags != 3 {
					t.Errorf("flags=%d", flags)
				}
				switch tc.name {
				case "black_bundle", "blue_shulker_box":
					if len(squares) != 2 || len(requirements) != 2 {
						t.Fatalf("squares=%v requirements=%v", squares, requirements)
					}
					want := registry.ItemBlackBundle
					if tc.name == "blue_shulker_box" {
						want = registry.ItemBlueShulkerBox
					}
					if result != fmt.Sprintf("stack:%d:1", want) {
						t.Errorf("result=%s", result)
					}
				case "map_cloning":
					if len(squares) != int(i)+2 || len(requirements) != 9 {
						t.Errorf("variant %d: squares=%v requirements=%v", i, squares, requirements)
					}
					if !strings.HasPrefix(result, "composite:[") || !strings.Contains(result, fmt.Sprintf("stack:%d:%d", registry.ItemFilledMap, i+2)) {
						t.Errorf("result=%s", result)
					}
				case "leather_boots_dyed":
					want := fmt.Sprintf("dyed:[component:[tag:minecraft:dyes]:%d tag:ids:[%d]]", 46, registry.ItemLeatherBoots)
					if result != want || len(squares) != 2 || !strings.HasPrefix(squares[1], "component:[tag:minecraft:dyes]") {
						t.Errorf("squares=%v result=%s, want %s", squares, result, want)
					}
				case "tipped_arrow":
					if len(squares) != 9 || squares[4] != fmt.Sprintf("potion:[tag:ids:[%d]]", registry.ItemLingeringPotion) || result != fmt.Sprintf("potion:[stack:%d:8]", registry.ItemTippedArrow) {
						t.Errorf("squares=%v result=%s", squares, result)
					}
				}
			}
			if r.ReadBool() {
				t.Error("grant replaces book")
			}
			if r.Err() != nil || r.Remaining() != 0 {
				t.Fatalf("leftover=%d err=%v", r.Remaining(), r.Err())
			}
			taken := runRecipe(c, "recipe take @s minecraft:"+tc.name)
			servertest.CheckCommandFeedback(t, taken.chats[0], "commands.recipe.take.success.single", servertest.NumberText(int64(tc.entries)), servertest.PlayerDisplayComponent("Tester"))
			if len(taken.removes) != 1 {
				t.Fatalf("remove packets=%d", len(taken.removes))
			}
			rm := protocol.NewReader(taken.removes[0])
			if n := rm.ReadVarInt(); n != tc.entries {
				t.Fatalf("remove entries=%d", n)
			}
			for _, id := range ids {
				if got := rm.ReadVarInt(); got != id {
					t.Errorf("remove id=%d, want %d", got, id)
				}
			}
			if rm.Err() != nil || rm.Remaining() != 0 {
				t.Fatalf("remove leftover=%d err=%v", rm.Remaining(), rm.Err())
			}
		})
	}
}

func readDynamicSet(r *protocol.Reader) string {
	n := r.ReadVarInt()
	if n == 0 {
		return r.ReadString(32767)
	}
	var ids []int32
	for range n - 1 {
		ids = append(ids, r.ReadVarInt())
	}
	return fmt.Sprintf("ids:%v", ids)
}

func readDynamicSlot(t *testing.T, r *protocol.Reader) string {
	t.Helper()
	switch kind := r.ReadVarInt(); kind {
	case 0:
		return "empty"
	case 4:
		return fmt.Sprintf("item:%d", r.ReadVarInt())
	case 5:
		item, count := r.ReadVarInt(), r.ReadVarInt()
		if pos, neg := r.ReadVarInt(), r.ReadVarInt(); pos != 0 || neg != 0 {
			t.Fatalf("unexpected stack patch %d/%d", pos, neg)
		}
		return fmt.Sprintf("stack:%d:%d", item, count)
	case 6:
		return "tag:" + readDynamicSet(r)
	case 2:
		return "potion:[" + readDynamicSlot(t, r) + "]"
	case 3:
		source := readDynamicSlot(t, r)
		return fmt.Sprintf("component:[%s]:%d", source, r.ReadVarInt())
	case 7:
		dye, target := readDynamicSlot(t, r), readDynamicSlot(t, r)
		return fmt.Sprintf("dyed:[%s %s]", dye, target)
	case 10:
		var parts []string
		for range r.ReadVarInt() {
			parts = append(parts, readDynamicSlot(t, r))
		}
		return fmt.Sprintf("composite:%v", parts)
	default:
		t.Fatalf("unknown slot display %d", kind)
		return ""
	}
}

func TestBlackDyeAdvancementUnlocksBundleRecipe(t *testing.T) {
	srv, c := servertest.Join(t, servertest.SurvivalTestConfig(), "Tester", servertest.OpsListing(t, "Tester"))
	c.WatchBook = true
	servertest.HoldTicks(t, srv)
	srv.GiveSlot("Tester", 36, servertest.ItemStack(t, "minecraft:black_dye", 1))
	want := registry.CraftingDisplayOfName("minecraft:black_bundle").ID
	found := false
	c.ReadTicks(srv, 2, func(id int32, data []byte) bool {
		if id == protocol.IDPlayRecipeBookAdd {
			r := protocol.NewReader(data)
			for range r.ReadVarInt() {
				display := r.ReadVarInt()
				switch kind := r.ReadVarInt(); kind {
				case 0:
				case 1:
					r.ReadVarInt()
					r.ReadVarInt()
				default:
					t.Fatalf("unexpected recipe display type %d", kind)
				}
				for range r.ReadVarInt() {
					readDynamicSlot(t, r)
				}
				readDynamicSlot(t, r)
				readDynamicSlot(t, r)
				r.ReadVarInt()
				r.ReadVarInt()
				if r.ReadBool() {
					for range r.ReadVarInt() {
						readDynamicSet(r)
					}
				}
				r.ReadU8()
				if display == want {
					found = true
				}
			}
			r.ReadBool()
			if r.Err() != nil || r.Remaining() != 0 {
				t.Fatalf("reward leftover=%d err=%v", r.Remaining(), r.Err())
			}
		}
		return false
	})
	if !found {
		t.Error("black dye did not unlock the black bundle display")
	}
	if !srv.AdvancementObtained("Tester", "minecraft:recipes/tools/black_bundle", "has_black_dye") {
		t.Error("black dye criterion not obtained")
	}
}
