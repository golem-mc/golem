package storage

import (
	"bytes"
	"compress/zlib"
	"encoding/binary"
	"os"
	"path/filepath"
	"testing"

	"github.com/golem-mc/golem/internal/config"
	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/internal/server/servertest"
	"github.com/golem-mc/golem/internal/server/world"
	"github.com/golem-mc/golem/nbt"
)

// TestAPhantomSavedBefore1_21_5KeepsItsSize loads an entity file written at
// DataVersion 4189, where a phantom's size was the key Size, and reads the
// size back through /data.
// vanilla: InlineBlockPosFormatFix
func TestAPhantomSavedBefore1_21_5KeepsItsSize(t *testing.T) {
	t.Parallel()
	cfg := config.Default()
	cfg.OnlineMode = false
	cfg.LevelName = t.TempDir()

	phantom := nbt.Compound{
		"id":   nbt.String("minecraft:phantom"),
		"Pos":  nbt.List{Elem: nbt.TypeDouble, Items: []nbt.Tag{nbt.Double(2.5), nbt.Double(80), nbt.Double(2.5)}},
		"UUID": nbt.IntArray{1, 2, 3, 4},
		"Size": nbt.Int(5), "AX": nbt.Int(1), "AY": nbt.Int(80), "AZ": nbt.Int(2),
		"NoAI": nbt.Byte(1),
	}
	record, err := nbt.Encode("", nbt.Compound{
		"DataVersion": nbt.Int(4189),
		"Position":    nbt.IntArray{0, 0},
		"Entities":    nbt.List{Elem: nbt.TypeCompound, Items: []nbt.Tag{phantom}},
	})
	if err != nil {
		t.Fatal(err)
	}
	var z bytes.Buffer
	zw := zlib.NewWriter(&z)
	zw.Write(record)
	zw.Close()
	sector := make([]byte, 8192)
	sector[2], sector[3] = 2, 1 // chunk (0,0) at sector 2, one sector long
	body := binary.BigEndian.AppendUint32(nil, uint32(z.Len()+1))
	body = append(body, 2)
	body = append(body, z.Bytes()...)
	path := servertest.EntityRegionPath(cfg.LevelName, world.ChunkPos{})
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, append(sector, append(body, make([]byte, 4096-len(body))...)...), 0o644); err != nil {
		t.Fatal(err)
	}

	srv, c := servertest.Join(t, cfg, "Keeper", servertest.OpsListing(t, "Keeper"))
	servertest.HoldTicks(t, srv)
	c.ChatCommand("data get entity @e[type=minecraft:phantom,limit=1] size")
	var got string
	c.SettleAndCollect(func(id int32, data []byte) {
		if id != protocol.IDPlaySystemChat {
			return
		}
		if text, ok := protocol.ReadText(protocol.NewReader(data)); ok && text.Translate == "commands.data.entity.query" {
			got = ""
			for _, e := range text.With[len(text.With)-1].Extra {
				got += e.Text
			}
		}
	})
	if got != "5" {
		t.Errorf("phantom size = %q, want 5", got)
	}
}
