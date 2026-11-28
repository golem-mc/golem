package network

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/golem-mc/golem/internal/config"
	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/internal/server"
	"github.com/golem-mc/golem/internal/server/servertest"
	"github.com/golem-mc/golem/nbt"
	"github.com/golem-mc/golem/registry"
)

// The pack registry tests are not parallel: the registry table is
// process-wide, so they run alone and the pack test puts it back after.

// TestConfigurationSendsPackRegistryEntries checks that a data pack's new
// painting variant and dialog reach Registry Data with their data after the
// built-ins, that an override of a built-in chat type carries the pack's
// data while an untouched entry carries none, that a pack tag can name the
// new entry, and that /dialog show resolves the pack's dialog.
// vanilla: RegistrySynchronization.packRegistry, RegistryDataLoader
func TestConfigurationSendsPackRegistryEntries(t *testing.T) {
	t.Cleanup(func() {
		registry.ApplyPackRegistries(nil)
		registry.ApplyPackTags(nil)
	})
	cfg := config.Default()
	cfg.OnlineMode = false
	cfg.LevelName = t.TempDir()
	pack := filepath.Join(cfg.LevelName, "datapacks", "p")
	writeFiles(t, map[string]string{
		filepath.Join(pack, "pack.mcmeta"):                             `{"pack":{"description":"p"}}`,
		filepath.Join(pack, "data/pk/painting_variant/wide.json"):      `{"asset_id":"pk:wide","width":3,"height":1}`,
		filepath.Join(pack, "data/pk/dialog/hello.json"):               `{"type":"minecraft:notice","title":"Hello"}`,
		filepath.Join(pack, "data/pk/tags/painting_variant/mine.json"): `{"values":["pk:wide"]}`,
		filepath.Join(pack, "data/minecraft/chat_type/chat.json"): `{
			"chat":{"parameters":["sender","content"],"translation_key":"pk.chat"},
			"narration":{"parameters":["sender","content"],"translation_key":"chat.type.text.narrate"}}`,
	})

	_, c := servertest.Dial(t, cfg, servertest.OpsListing(t, "Testificate"))
	regs, tags := configureReading(c, "Testificate")

	paintings := regs["minecraft:painting_variant"]
	builtin, _ := registryBuiltin("minecraft:painting_variant")
	if len(paintings) != len(builtin)+1 || paintings[len(paintings)-1].name != "pk:wide" {
		t.Fatalf("painting_variant has %d entries ending %v, want the %d built-ins then pk:wide", len(paintings), paintings[len(paintings)-1].name, len(builtin))
	}
	wide, _ := paintings[len(paintings)-1].data.(nbt.Compound)
	if w, _ := nbt.Get[nbt.Int](wide, "width"); w != 3 {
		t.Errorf("pk:wide data = %v, want width 3", wide)
	}
	for _, e := range paintings {
		if e.name == "minecraft:kebab" && e.data != nil {
			t.Errorf("minecraft:kebab carries data %v, want none", e.data)
		}
	}
	var chatKey nbt.String
	for _, e := range regs["minecraft:chat_type"] {
		if e.name == "minecraft:chat" {
			chat, _ := e.data.(nbt.Compound)
			inner, _ := nbt.Get[nbt.Compound](chat, "chat")
			chatKey, _ = nbt.Get[nbt.String](inner, "translation_key")
		}
	}
	if chatKey != "pk.chat" {
		t.Errorf("minecraft:chat translation_key = %q, want pk.chat", chatKey)
	}
	if got := tags["pk:mine"]; len(got) != 1 || got[0] != int32(len(builtin)) {
		t.Errorf("painting_variant tag pk:mine = %v, want [%d]", got, len(builtin))
	}

	c.Playing = true
	c.EnterWorld("Testificate")
	c.ChatCommand("dialog show @s pk:hello")
	data, ok := servertest.ReadUntil(t, c, servertest.ExpectTimeout, protocol.IDPlayShowDialog)
	if !ok {
		t.Fatal("no Show Dialog packet")
	}
	dialogs, _ := registryBuiltin("minecraft:dialog")
	// A registry holder travels as its index plus one.
	if want := []byte{byte(len(dialogs) + 1)}; !bytes.Equal(data, want) {
		t.Errorf("Show Dialog = % x, want % x (pk:hello after the built-ins)", data, want)
	}
}

// TestConfigurationSendsNoRegistryDataWithoutPacks checks that with no data
// pack every Registry Data entry leaves its data to the client's core pack.
func TestConfigurationSendsNoRegistryDataWithoutPacks(t *testing.T) {
	cfg := config.Default()
	cfg.OnlineMode = false
	cfg.LevelName = t.TempDir()
	_, c := servertest.Dial(t, cfg)
	regs, _ := configureReading(c, "Testificate")
	if len(regs) != len(registry.Synchronized) {
		t.Errorf("got %d registries, want %d", len(regs), len(registry.Synchronized))
	}
	for name, entries := range regs {
		for _, e := range entries {
			if e.data != nil {
				t.Errorf("%s entry %s carries data", name, e.name)
			}
		}
	}
}

// TestInvalidPackRegistryEntryStopsStartup checks that a pack entry of a
// synchronized registry the client's codec refuses fails the start naming
// the entry, as vanilla's world loading does, instead of disconnecting every
// client at the end of configuration.
// vanilla: RegistryDataLoader.load
func TestInvalidPackRegistryEntryStopsStartup(t *testing.T) {
	t.Cleanup(func() { registry.ApplyPackRegistries(nil) })
	for _, tc := range []struct{ name, path, json, want string }{
		{"painting width", "painting_variant", `{"asset_id":"minecraft:kebab","width":0,"height":1}`, "range [1;16]"},
		{"painting asset", "painting_variant", `{"width":1,"height":1}`, "asset_id"},
		{"jukebox length", "jukebox_song", `{"sound_event":"minecraft:music_disc.13","description":"d","length_in_seconds":0,"comparator_output":1}`, "length_in_seconds"},
		{"jukebox output", "jukebox_song", `{"sound_event":"minecraft:music_disc.13","description":"d","length_in_seconds":1,"comparator_output":16}`, "comparator_output"},
		{"instrument range", "instrument", `{"sound_event":"minecraft:item.goat_horn.sound.0","use_duration":1,"range":0,"description":"d"}`, "range"},
		{"damage type", "damage_type", `{"message_id":"x","scaling":"sideways","exhaustion":0}`, "scaling"},
		{"pig model", "pig_variant", `{"asset_id":"a","model":"warm","baby_asset_id":"b","spawn_conditions":[]}`, "model"},
		{"chat parameter", "chat_type", `{"chat":{"translation_key":"k","parameters":["nope"]},"narration":{"translation_key":"k","parameters":[]}}`, "parameters"},
		{"painting boolean width", "painting_variant", `{"asset_id":"minecraft:kebab","width":true,"height":1}`, "width"},
		{"painting namespace", "painting_variant", `{"asset_id":"..:kebab","width":1,"height":1}`, "asset_id"},
		{"wolf spawn", "wolf_variant", `{"assets":{"wild":"a","tame":"b","angry":"c"},"baby_assets":{"wild":"a","tame":"b","angry":"c"}}`, "spawn_conditions"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			cfg := config.Default()
			cfg.OnlineMode = false
			cfg.LevelName = t.TempDir()
			pack := filepath.Join(cfg.LevelName, "datapacks", "p")
			writeFiles(t, map[string]string{
				filepath.Join(pack, "pack.mcmeta"):                  `{"pack":{"description":"p"}}`,
				filepath.Join(pack, "data/zz", tc.path, "bad.json"): tc.json,
			})
			srv, err := server.New(cfg)
			if err == nil {
				srv.Shutdown()
				t.Fatal("server started with the invalid entry")
			}
			if !strings.Contains(err.Error(), "zz:bad") || !strings.Contains(err.Error(), tc.want) {
				t.Errorf("start error = %v, want it to name zz:bad and %q", err, tc.want)
			}
		})
	}
}

type registryEntry struct {
	name string
	data nbt.Tag
}

// registryBuiltin is a registry's built-in entries.
func registryBuiltin(name string) ([]string, bool) {
	for _, r := range registry.Synchronized {
		if r.Name == name {
			return r.Entries, true
		}
	}
	return nil, false
}

// configureReading runs configuration up to Finish Configuration, returning
// the Registry Data entries by registry and Update Tags' painting_variant
// tags.
func configureReading(c *servertest.Client, name string) (map[string][]registryEntry, map[string][]int32) {
	t := c.T
	t.Helper()
	c.Login(name)
	c.Expect(protocol.IDConfigPluginMessage)
	c.Expect(protocol.IDKnownPacks)
	c.Send(protocol.IDKnownPacksIn, func(b *protocol.Buffer) {
		b.PutVarInt(1)
		b.PutString("minecraft")
		b.PutString("core")
		b.PutString(registry.Version)
	})
	regs := map[string][]registryEntry{}
	var tags map[string][]int32
	for {
		id, data, err := c.Read()
		if err != nil {
			t.Fatalf("read configuration packet: %v", err)
		}
		switch id {
		case protocol.IDRegistryData:
			r := protocol.NewReader(data)
			reg := r.ReadString(32767)
			entries := []registryEntry{}
			for range r.ReadVarInt() {
				e := registryEntry{name: r.ReadString(32767)}
				if r.ReadBool() {
					tag, n, err := nbt.DecodeNetwork(data[len(data)-r.Remaining():])
					if err != nil {
						t.Fatalf("%s entry %s: %v", reg, e.name, err)
					}
					r.Skip(n)
					e.data = tag
				}
				entries = append(entries, e)
			}
			if r.Err() != nil || r.Remaining() != 0 {
				t.Fatalf("registry %s: %v, %d bytes left", reg, r.Err(), r.Remaining())
			}
			regs[reg] = entries
		case protocol.IDConfigUpdateTags:
			tags = registryTagsOf(t, data, "minecraft:painting_variant")
		case protocol.IDConfigFinish:
			c.Send(protocol.IDConfigFinishAck, nil)
			return regs, tags
		}
	}
}

func writeFiles(t *testing.T, files map[string]string) {
	t.Helper()
	for path, body := range files {
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
}

// TestAPackPlaceableTagPicksThePlacedPainting hangs a plain painting item
// with a pack whose minecraft:placeable tag holds only its 3x1 pk:wide: the
// roll draws from the pack's tag, so pk:wide hangs on a wall a built-in 1x1
// would fit too.
// vanilla: Painting.create reads PaintingVariantTags.PLACEABLE from the registry
func TestAPackPlaceableTagPicksThePlacedPainting(t *testing.T) {
	t.Cleanup(func() {
		registry.ApplyPackRegistries(nil)
		registry.ApplyPackTags(nil)
	})
	cfg := config.Default()
	cfg.OnlineMode = false
	cfg.GameMode = "creative"
	cfg.LevelName = t.TempDir()
	pack := filepath.Join(cfg.LevelName, "datapacks", "p")
	writeFiles(t, map[string]string{
		filepath.Join(pack, "pack.mcmeta"):                                         `{"pack":{"description":"p"}}`,
		filepath.Join(pack, "data/pk/painting_variant/wide.json"):                  `{"asset_id":"pk:wide","width":3,"height":1}`,
		filepath.Join(pack, "data/minecraft/tags/painting_variant/placeable.json"): `{"replace":true,"values":["pk:wide"]}`,
	})

	srv, c := servertest.Join(t, cfg, "Testificate")
	wide := servertest.PaintingVariantID(t, "pk:wide")
	c.SetCreativeSlot(36, protocol.ItemStack{ItemID: servertest.PaintingItem(t), Count: 1})
	c.Settle()

	for x := int32(3); x <= 5; x++ {
		srv.SetBlock(x, 64, 5, servertest.PaintingWallState)
	}
	c.MoveToGround(4.5, 64, 3.5, true)
	c.Settle()
	c.PlaceBlock(4, 64, 5, protocol.FaceNorth)
	painting := c.AwaitSpawn(protocol.EntityTypePainting)
	entries := c.AwaitMetadataFor(painting.EntityID)
	if field := servertest.MetaOf(entries, protocol.MetadataIndexPaintingVariant); field == nil || field.VarintVal != wide+1 {
		t.Errorf("the placed painting's variant = %v, want pk:wide (%d), the pack tag's only entry", field, wide+1)
	}
}
