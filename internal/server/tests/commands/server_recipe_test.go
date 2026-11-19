package commands

import (
	"bytes"
	"testing"

	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/internal/server"
	"github.com/golem-mc/golem/internal/server/servertest"
	"github.com/golem-mc/golem/registry"
)

// oakBoatDisplay is the display ID of minecraft:oak_boat, a recipe a new
// player does not know.
var oakBoatDisplay = registry.CraftingDisplayOfName("minecraft:oak_boat").ID

// recipeRun is what one /recipe command sent back.
type recipeRun struct {
	adds    [][]byte
	removes [][]byte
	chats   [][]byte
}

func (r recipeRun) said(key string) bool {
	for _, chat := range r.chats {
		if bytes.Contains(chat, []byte(key)) {
			return true
		}
	}
	return false
}

// runRecipe runs command and reads up to its answer in the chat. The server
// sends the book packet before the answer, so this reads all the command
// sent; a ping settle would not, since the ping can be answered before the
// command runs and leave its packets for the next call.
func runRecipe(c *servertest.Client, command string) recipeRun {
	c.T.Helper()
	var got recipeRun
	c.ChatCommand(command)
	for id, data := range c.Packets(servertest.ExpectTimeout) {
		switch id {
		case protocol.IDPlayRecipeBookAdd:
			got.adds = append(got.adds, data)
		case protocol.IDPlayRecipeBookRemove:
			got.removes = append(got.removes, data)
		case protocol.IDPlaySystemChat:
			got.chats = append(got.chats, data)
			return got
		}
	}
	c.T.Fatalf("no answer to /%s within %s", command, servertest.ExpectTimeout)
	return got
}

func recipeClient(t *testing.T) (*server.Server, *servertest.Client) {
	t.Helper()
	srv, c := servertest.Dial(t, servertest.SurvivalTestConfig(), servertest.OpsListing(t, "DJ"))
	c.Login("DJ")
	c.WatchBook = true
	c.Configure()
	c.EnterWorldAs("DJ")
	c.Settle()
	return srv, c
}

// TestRecipeGiveAndTakeSendTheBookPackets gives a recipe the player does not
// know, which arrives as a Recipe Book Add that adds to the book rather than
// replacing it, then takes it back with a Recipe Book Remove naming its
// display. Repeating either changes nothing and fails.
func TestRecipeGiveAndTakeSendTheBookPackets(t *testing.T) {
	t.Parallel()
	_, c := recipeClient(t)

	got := runRecipe(c, "recipe give DJ minecraft:oak_boat")
	if len(got.adds) != 1 {
		t.Fatalf("give sent %d Recipe Book Adds, want 1", len(got.adds))
	}
	add := protocol.NewReader(got.adds[0])
	if n, id := add.ReadVarInt(), add.ReadVarInt(); n != 1 || id != oakBoatDisplay {
		t.Errorf("add = %d entries, first display %d, want 1 entry for %d", n, id, oakBoatDisplay)
	}
	if last := got.adds[0][len(got.adds[0])-1]; last != 0 {
		t.Errorf("add replace flag = %d, want 0: a grant adds to the book", last)
	}
	if flags := got.adds[0][len(got.adds[0])-2]; flags != 0x03 {
		t.Errorf("entry flags = %#x, want notified and highlighted", flags)
	}
	if !got.said("commands.recipe.give.success.single") {
		t.Error("give did not answer with commands.recipe.give.success.single")
	}

	got = runRecipe(c, "recipe give DJ minecraft:oak_boat")
	if len(got.adds) != 0 || !got.said("commands.recipe.give.failed") {
		t.Errorf("a repeated give sent %d adds, failed = %v, want none and the failure",
			len(got.adds), got.said("commands.recipe.give.failed"))
	}

	got = runRecipe(c, "recipe take DJ minecraft:oak_boat")
	if len(got.removes) != 1 {
		t.Fatalf("take sent %d Recipe Book Removes, want 1", len(got.removes))
	}
	want := protocol.Buffer{}
	want.PutVarInt(1)
	want.PutVarInt(oakBoatDisplay)
	if !bytes.Equal(got.removes[0], want.Bytes()) {
		t.Errorf("remove = %x, want %x", got.removes[0], want.Bytes())
	}
	if !got.said("commands.recipe.take.success.single") {
		t.Error("take did not answer with commands.recipe.take.success.single")
	}

	got = runRecipe(c, "recipe take DJ minecraft:oak_boat")
	if len(got.removes) != 0 || !got.said("commands.recipe.take.failed") {
		t.Errorf("a repeated take sent %d removes, failed = %v, want none and the failure",
			len(got.removes), got.said("commands.recipe.take.failed"))
	}
}

// TestRecipeStarGivesEveryRecipe unlocks every recipe kind: the packet carries every display the player did not already hold,
// and the feedback counts the recipes given.
func TestRecipeStarGivesEveryRecipe(t *testing.T) {
	t.Parallel()
	_, c := recipeClient(t)
	all := len(registry.CraftingDisplays) + len(registry.FurnaceDisplays) + len(registry.StonecutterRecipes) +
		len(registry.SmithingTransformRecipes) + len(registry.SmithingTrimRecipes)

	got := runRecipe(c, "recipe give DJ *")
	if len(got.adds) != 1 {
		t.Fatalf("give * sent %d adds, want 1", len(got.adds))
	}
	// The crafting table is the one recipe a new player already holds.
	if n, want := protocol.NewReader(got.adds[0]).ReadVarInt(), int32(all-1); n != want {
		t.Errorf("give * added %d entries, want %d", n, want)
	}
	servertest.ExpectFeedbackSoon(t, c, got.chats, "commands.recipe.give.success.single",
		servertest.NumberText(int64(all-1)), servertest.PlayerDisplayComponent("DJ"))

	got = runRecipe(c, "recipe take DJ *")
	if len(got.removes) != 1 {
		t.Fatalf("take * sent %d removes, want 1", len(got.removes))
	}
	if n, want := protocol.NewReader(got.removes[0]).ReadVarInt(), int32(all); n != want {
		t.Errorf("take * removed %d entries, want %d", n, want)
	}
}

// TestRecipeUnknownRecipeIsRefused names a recipe the table does not hold.
func TestRecipeUnknownRecipeIsRefused(t *testing.T) {
	t.Parallel()
	_, c := recipeClient(t)

	got := runRecipe(c, "recipe give DJ minecraft:no_such_recipe")
	if len(got.adds) != 0 || !got.said("recipe.notFound") {
		t.Errorf("unknown recipe: %d adds, notFound = %v", len(got.adds), got.said("recipe.notFound"))
	}
}

// TestRecipeGiveSurvivesARejoin keeps the granted recipe in the player's
// file: the next join's book names its display.
func TestRecipeGiveSurvivesARejoin(t *testing.T) {
	t.Parallel()
	cfg := servertest.SurvivalTestConfig()
	cfg.LevelName = t.TempDir()
	_, connect := servertest.Start(t, cfg, servertest.OpsListing(t, "DJ"))
	c := connect()
	c.Login("DJ")
	c.WatchBook = true
	c.Configure()
	c.EnterWorld("DJ")
	c.Settle()
	runRecipe(c, "recipe give DJ minecraft:oak_boat")
	c.Disconnect()

	c = connect()
	c.Login("DJ")
	c.WatchBook = true
	c.Configure()
	var joined [][]byte
	c.ReadJoin(func(id int32, data []byte) {
		if id == protocol.IDPlayRecipeBookAdd {
			joined = append(joined, data)
		}
	})
	if len(joined) != 1 {
		t.Fatalf("the join carried %d book adds, want 1", len(joined))
	}
	if n := protocol.NewReader(joined[0]).ReadVarInt(); n != 2 {
		t.Errorf("the rejoined book holds %d recipes, want the crafting table and the boat", n)
	}
}

func TestRecipeCommandUnlocksWorkstationRecipes(t *testing.T) {
	for _, tc := range []struct {
		name string
		kind int32
	}{
		{"minecraft:andesite_slab_from_andesite_stonecutting", 3},
		{"minecraft:netherite_sword_smithing", 4},
		{"minecraft:sentry_armor_trim_smithing_template_smithing_trim", 4},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			srv, c := recipeClient(t)
			c.ChatCommand("recipe give @s " + tc.name)
			data := c.ExpectWithinTicks(srv, 5, protocol.IDPlayRecipeBookAdd)
			r := protocol.NewReader(data)
			if n := r.ReadVarInt(); n != 1 {
				t.Fatalf("added %d recipes, want 1", n)
			}
			r.ReadVarInt()
			if kind := r.ReadVarInt(); kind != tc.kind {
				t.Errorf("display type = %d, want %d", kind, tc.kind)
			}
			if flags := data[len(data)-2]; flags != 3 {
				t.Errorf("entry flags = %d, want notification and highlight", flags)
			}
			c.ExpectSystemChat("commands.recipe.give.success.single", false, servertest.NumberText(1), servertest.PlayerDisplayComponent("DJ"))
			got := runRecipe(c, "recipe take @s "+tc.name)
			if len(got.removes) != 1 || !got.said("commands.recipe.take.success.single") {
				t.Fatalf("take = %+v, want removal and success", got)
			}
		})
	}
}
