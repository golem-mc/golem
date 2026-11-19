package network

import (
	"path/filepath"
	"slices"
	"testing"

	"github.com/golem-mc/golem/internal/config"
	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/internal/server/servertest"
	"github.com/golem-mc/golem/registry"
)

// TestConfigurationSendsPackTags checks that Update Tags carries a data pack's
// block tag next to the built-in ones, with a replaced tag cut down to the
// pack's entries.
// vanilla: TagLoader, TagNetworkSerialization
func TestConfigurationSendsPackTags(t *testing.T) {
	t.Parallel()
	cfg := config.Default()
	cfg.OnlineMode = false
	cfg.LevelName = t.TempDir()
	pack := filepath.Join(cfg.LevelName, "datapacks", "p")
	tags := filepath.Join(pack, "data", "pk", "tags", "block")
	files := map[string]string{
		filepath.Join(pack, "pack.mcmeta"):        `{"pack":{"description":"p"}}`,
		filepath.Join(tags, "wire_probe.json"):    `{"values":["minecraft:stone","#minecraft:dirt"]}`,
		filepath.Join(tags, "wire_replaced.json"): `{"replace":true,"values":["minecraft:stone"]}`,
		filepath.Join(tags, "wire_dropped.json"):  `{"values":["nope:missing"]}`,
	}
	writeFiles(t, files)

	_, c := servertest.Dial(t, cfg)
	c.Login("Testificate")
	c.Expect(protocol.IDConfigPluginMessage)
	c.Expect(protocol.IDKnownPacks)
	c.Send(protocol.IDKnownPacksIn, func(b *protocol.Buffer) {
		b.PutVarInt(1)
		b.PutString("minecraft")
		b.PutString("core")
		b.PutString(registry.Version)
	})
	var block map[string][]int32
	for {
		id, data, err := c.Read()
		if err != nil {
			t.Fatalf("read configuration packet: %v", err)
		}
		if id == protocol.IDConfigUpdateTags {
			block = registryTagsOf(t, data, "minecraft:block")
		}
		if id == protocol.IDConfigFinish {
			c.Send(protocol.IDConfigFinishAck, nil)
			break
		}
	}
	stone, _ := registry.BlockByName("minecraft:stone")
	dirt, _ := registry.BlockByName("minecraft:dirt")
	if got := block["pk:wire_probe"]; !containsID(got, int32(stone)) || !containsID(got, int32(dirt)) {
		t.Errorf("pk:wire_probe = %v, want stone and dirt (%d, %d)", got, stone, dirt)
	}
	if got := block["pk:wire_replaced"]; len(got) != 1 || got[0] != int32(stone) {
		t.Errorf("pk:wire_replaced = %v, want just stone", got)
	}
	if _, ok := block["pk:wire_dropped"]; ok {
		t.Error("a tag with a missing required entry was sent")
	}
	if _, ok := block["minecraft:logs"]; !ok {
		t.Error("the built-in logs tag went missing")
	}
}

func containsID(ids []int32, id int32) bool {
	return slices.Contains(ids, id)
}

// registryTagsOf reads one registry's section of an Update Tags payload.
func registryTagsOf(t *testing.T, data []byte, want string) map[string][]int32 {
	t.Helper()
	r := protocol.NewReader(data)
	out := map[string][]int32{}
	for range r.ReadVarInt() {
		reg := r.ReadString(32767)
		for range r.ReadVarInt() {
			name := r.ReadString(32767)
			n := r.ReadVarInt()
			ids := make([]int32, 0, n)
			for range n {
				ids = append(ids, r.ReadVarInt())
			}
			if reg == want {
				out[name] = ids
			}
		}
	}
	if r.Err() != nil {
		t.Fatal(r.Err())
	}
	return out
}
