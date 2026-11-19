package containers

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/internal/server/player"
	"github.com/golem-mc/golem/internal/server/servertest"
	"github.com/golem-mc/golem/nbt"
	"github.com/golem-mc/golem/registry"
)

// The synchronized registries are process-wide, so this test is not parallel.
func TestAJukeboxPlaysTheStacksPackSong(t *testing.T) {
	t.Cleanup(func() { registry.ApplyPackRegistries(nil) })
	cfg := servertest.SurvivalTestConfig()
	cfg.LevelName = t.TempDir()
	pack := filepath.Join(cfg.LevelName, "datapacks", "p")
	for file, body := range map[string]string{
		"pack.mcmeta":                     `{"pack":{"description":"p"}}`,
		"data/pk/jukebox_song/short.json": `{"sound_event":"minecraft:music_disc.13","description":"x","length_in_seconds":1.0,"comparator_output":9}`,
	} {
		path := filepath.Join(pack, file)
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	srv, c := servertest.Join(t, cfg, "Testificate", servertest.OpsListing(t, "Testificate"))
	servertest.HoldTicks(t, srv)
	srv.SetBlock(3, 64, 0, registry.BlockStates["minecraft:jukebox"])
	servertest.Setblock(t, c, 3, 64, -1, "minecraft:comparator[facing=south]")
	c.ChatCommand(`give @s minecraft:stick[jukebox_playable="pk:short"]`)
	c.Settle()
	stick := srv.InventorySlot("Testificate", player.HotbarSlot)
	if stick.Empty() || registry.Item(stick.ItemID) != registry.ItemStick {
		t.Fatalf("/give playable stick left hand %+v", stick)
	}
	c.UseBlock(3, 64, 0)
	w := &servertest.WindowedClient{Client: c, States: map[int32]int32{}}
	_, _, _, id := awaitJukeboxEvent(t, w, protocol.WorldEventJukeboxPlay)
	if want := registry.JukeboxSongID("pk:short"); id != want || want < 0 {
		t.Fatalf("play event song = %d, want pk:short (%d)", id, want)
	}
	started := srv.TicksRun()
	servertest.UntilTicks(t, srv, 5, "comparator reads pack song", func() bool {
		tag, _ := srv.BlockEntityTagAt(3, 64, -1)
		output, _ := nbt.IntField(tag, "OutputSignal")
		return output == 9
	})
	servertest.RunTicks(t, srv, int(39-(srv.TicksRun()-started)))
	tag, _ := srv.BlockEntityTagAt(3, 64, 0)
	if _, playing := tag["ticks_since_song_started"]; !playing {
		t.Fatal("pack song finished before its length plus padding")
	}
	servertest.UntilTicks(t, srv, 6, "pack song finishes within 40+5 ticks", func() bool {
		tag, _ := srv.BlockEntityTagAt(3, 64, 0)
		_, playing := tag["ticks_since_song_started"]
		return !playing
	})
	_, props := servertest.StatePropsOf(t, srv, 3, 64, 0)
	if props["has_record"] != "true" || srv.InventoryAt(3, 64, 0)[0].Empty() {
		t.Fatal("finished song must keep the record")
	}
	awaitJukeboxEvent(t, w, protocol.WorldEventJukeboxStop)
	c.UseBlock(3, 64, 0)
	c.Settle()
	c.ChatCommand(`give @s minecraft:music_disc_13[jukebox_playable="minecraft:cat"]`)
	c.Settle()
	c.UseBlock(3, 64, 0)
	_, _, _, id = awaitJukeboxEvent(t, w, protocol.WorldEventJukeboxPlay)
	if want := registry.JukeboxSongID("minecraft:cat"); id != want {
		t.Errorf("overridden disc plays %d, want cat (%d)", id, want)
	}
	c.UseBlock(3, 64, 0)
	awaitJukeboxEvent(t, w, protocol.WorldEventJukeboxStop)
	c.ChatCommand(`clear @s`)
	c.ChatCommand(`give @s minecraft:music_disc_13[!jukebox_playable]`)
	c.Settle()
	disc := srv.InventorySlot("Testificate", player.HotbarSlot)
	if disc.Empty() || !disc.Removes(protocol.DataComponentJukeboxPlayable) {
		t.Fatal("/give did not keep the song removal")
	}
	c.UseBlock(3, 64, 0)
	c.Settle()
	_, props = servertest.StatePropsOf(t, srv, 3, 64, 0)
	if props["has_record"] != "false" || !srv.InventorySlot("Testificate", player.HotbarSlot).Equal(disc) {
		t.Fatal("jukebox accepted a disc with its song removed")
	}
}
