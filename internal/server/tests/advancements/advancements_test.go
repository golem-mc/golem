package advancements

import (
	"encoding/json"
	"maps"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/internal/server/player"
	"github.com/golem-mc/golem/internal/server/servertest"
)

// The advancements a player's screen shows, the criteria that grant them,
// and the progress file that keeps them across a rejoin.

// storyTab is what Update Advancements adds once the story root is done:
// the root, its child and its grandchild, the depth a done advancement
// shows below it. vanilla: AdvancementVisibilityEvaluator.VISIBILITY_DEPTH
var storyTab = []string{"minecraft:story/root", "minecraft:story/mine_stone", "minecraft:story/upgrade_tools"}

// firstTickRecipe is what the join's first tick shows: the crafting table
// recipe, which minecraft:tick unlocks, and its root, a done advancement
// showing its ancestors even without a display.
// vanilla: AdvancementVisibilityEvaluator.evaluateVisibility
var firstTickRecipe = []string{"minecraft:recipes/decorations/crafting_table", "minecraft:recipes/root"}

// storyAdded lists the story tab's advancements an update adds.
func storyAdded(u sentUpdate) []string {
	var ids []string
	for id := range u.added {
		if strings.HasPrefix(id, "minecraft:story/") {
			ids = append(ids, id)
		}
	}
	slices.Sort(ids)
	return ids
}

// expectStoryRoot checks an update that shows the story tab with its root
// obtained.
func expectStoryRoot(t *testing.T, u sentUpdate) {
	t.Helper()
	for _, id := range storyTab {
		if _, ok := u.added[id]; !ok {
			t.Errorf("Update Advancements did not add %s (added %d)", id, len(u.added))
		}
	}
	if _, ok := u.added["minecraft:story/smelt_iron"]; ok {
		t.Error("Update Advancements added story/smelt_iron, three below the only done advancement")
	}
	if story := storyAdded(u); len(story) != len(storyTab) {
		t.Errorf("Update Advancements added story tab advancements %v, want the story tab's %d", story, len(storyTab))
	}
	root := u.added["minecraft:story/root"]
	if root.parent != "" || !root.display || root.title != "advancements.story.root.title" ||
		root.background != "minecraft:gui/advancements/backgrounds/stone" || root.frame != 0 || !root.telemetry {
		t.Errorf("story/root = %+v, want the parentless task with the stone background", root)
	}
	// show_toast false and a background: flags 0x01.
	if root.flags != 0x01 {
		t.Errorf("story/root's flags = %#x, want 0x01", root.flags)
	}
	if root.icon != servertest.ItemIDOrDie(t, "minecraft:grass_block") || root.count != 1 {
		t.Errorf("story/root's icon = %d x%d, want one grass block", root.icon, root.count)
	}
	if !slices.EqualFunc(root.requirements, [][]string{{"crafting_table"}}, slices.Equal) {
		t.Errorf("story/root's requirements = %v, want [[crafting_table]]", root.requirements)
	}
	if mine := u.added["minecraft:story/mine_stone"]; mine.parent != "minecraft:story/root" || mine.x != 1 {
		t.Errorf("story/mine_stone = %+v, want story/root's child one column right", mine)
	}
	if got := u.progress["minecraft:story/root"]["crafting_table"]; got == nil {
		t.Errorf("story/root's progress = %v, want crafting_table obtained", u.progress["minecraft:story/root"])
	}
	if got, ok := u.progress["minecraft:story/mine_stone"]["get_stone"]; !ok || got != nil {
		t.Errorf("story/mine_stone's progress = %v, want get_stone not obtained", u.progress["minecraft:story/mine_stone"])
	}
	if !u.show {
		t.Error("Update Advancements' show advancements is false")
	}
}

// TestACraftingTableShowsTheStoryTab joins a fresh player, whose screen
// shows only the recipe the first tick unlocks, then hands them a crafting
// table: the story root's criterion is obtained and the story tab arrives,
// not as a reset since the join's flush already went by.
// vanilla: PlayerAdvancements.flushDirty, InventoryChangeTrigger
func TestACraftingTableShowsTheStoryTab(t *testing.T) {
	t.Parallel()
	srv, connect := servertest.Start(t, servertest.SurvivalTestConfig())
	c, update := join(t, connect, "Crafter")
	first := joinUpdate(t, srv, c, update)
	if !first.reset {
		t.Error("the join's first Update Advancements does not reset the client's tree")
	}
	if added := slices.Sorted(maps.Keys(first.added)); !slices.Equal(added, firstTickRecipe) {
		t.Errorf("a fresh player's first update added %v, want %v", added, firstTickRecipe)
	}
	if done := first.done(); !slices.Equal(done, firstTickRecipe[:1]) {
		t.Errorf("a fresh player's first update has %v done, want %v", done, firstTickRecipe[:1])
	}
	c.ExpectNoneWithinTicks(srv, 2, servertest.PacketIs(protocol.IDPlayUpdateAdvancements))

	srv.GiveSlot("Crafter", player.HotbarSlot, servertest.ItemStack(t, "minecraft:crafting_table", 1))
	u := awaitUpdate(t, srv, c)
	if u.reset {
		t.Error("the update after the join's flush resets the client's tree")
	}
	expectStoryRoot(t, u)
	if len(u.progress) != len(storyTab) {
		t.Errorf("progress for %d advancements, want the %d added", len(u.progress), len(storyTab))
	}

	// Holding another item fires the trigger again with nothing new to
	// obtain, so nothing is sent.
	srv.GiveSlot("Crafter", player.HotbarSlot+1, servertest.ItemStack(t, "minecraft:dirt", 1))
	c.ExpectNoneWithinTicks(srv, 4, servertest.PacketIs(protocol.IDPlayUpdateAdvancements))
}

// TestAdvancementsSurviveARejoin obtains the story root, leaves, and finds
// it in players/advancements/<uuid>.json; the rejoin's first Update
// Advancements resets the client's tree and shows the story tab again.
// vanilla: PlayerAdvancements.save, load
func TestAdvancementsSurviveARejoin(t *testing.T) {
	t.Parallel()
	cfg := servertest.SurvivalTestConfig()
	cfg.LevelName = t.TempDir()
	srv, connect := servertest.Start(t, cfg)
	c, _ := join(t, connect, "Keeper")
	srv.GiveSlot("Keeper", player.HotbarSlot, servertest.ItemStack(t, "minecraft:crafting_table", 1))
	awaitUpdate(t, srv, c)
	c.Disconnect()

	data, err := os.ReadFile(filepath.Join(cfg.LevelName, "players", "advancements", protocol.OfflineUUID("Keeper").String()+".json"))
	if err != nil {
		t.Fatalf("read the advancements file: %v", err)
	}
	var file map[string]json.RawMessage
	if err := json.Unmarshal(data, &file); err != nil {
		t.Fatalf("parse the advancements file: %v\n%s", err, data)
	}
	var root struct {
		Criteria map[string]string `json:"criteria"`
		Done     bool              `json:"done"`
	}
	if err := json.Unmarshal(file["minecraft:story/root"], &root); err != nil || !root.Done || root.Criteria["crafting_table"] == "" {
		t.Errorf("the file's story/root = %s, want done with crafting_table's time", file["minecraft:story/root"])
	}
	if string(file["DataVersion"]) != "5023" {
		t.Errorf("the file's DataVersion = %s, want 26.3's 5023", file["DataVersion"])
	}
	if _, ok := file["minecraft:story/mine_stone"]; ok {
		t.Error("the file keeps story/mine_stone, which has nothing obtained")
	}

	again, update := join(t, connect, "Keeper")
	if update == nil {
		update = again.ExpectWithinTicks(srv, 40, protocol.IDPlayUpdateAdvancements)
	}
	u := parseUpdate(t, update)
	if !u.reset {
		t.Error("the rejoin's first Update Advancements does not reset the client's tree")
	}
	expectStoryRoot(t, u)
}

// seenTab sends Seen Advancements opening the given tab.
func seenTab(c *servertest.Client, tab string) {
	c.Send(protocol.IDPlaySeenAdvancements, func(b *protocol.Buffer) {
		b.PutVarInt(0)
		b.PutString(tab)
	})
}

// readSelectedTab reads Select Advancements Tab's root, empty for none.
func readSelectedTab(t *testing.T, data []byte) string {
	t.Helper()
	r := protocol.NewReader(data)
	tab := ""
	if r.ReadBool() {
		tab = r.ReadString(32767)
	}
	if err := r.Err(); err != nil || r.Remaining() != 0 {
		t.Fatalf("select advancements tab: %v, %d bytes left", err, r.Remaining())
	}
	return tab
}
