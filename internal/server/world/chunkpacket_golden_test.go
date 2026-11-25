package world

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"os"
	"strings"
	"testing"

	"github.com/golem-mc/golem/registry"
)

// Scaffolding for one review: records the chunk packets of 3, 16 and 17 block
// entities and a sign edit. Set GOLDEN_UPDATE=1 to write the file.
func TestChunkPacketGolden(t *testing.T) {
	w, err := NewWorld(t.TempDir(), NewFlatGenerator())
	if err != nil {
		t.Fatal(err)
	}
	defer w.Close()
	c, _ := w.Acquire(ChunkPos{})
	var out strings.Builder
	record := func(what string) {
		sum := sha256.Sum256(w.CachedChunkPacket(c).Data())
		fmt.Fprintf(&out, "%s %s\n", what, hex.EncodeToString(sum[:]))
	}
	w.SetBlock(4, 70, 5, registry.BlockStates["minecraft:oak_sign"])
	be := w.EnsureBlockEntity(4, 70, 5, KindSign)
	for i := range int32(17) {
		x, z := i%8, 10+i/8
		w.SetBlock(x, 71, z, registry.BlockStates["minecraft:chest"])
		w.EnsureBlockEntity(x, 71, z, KindChest)
		if n := i + 2; n == 3 || n == 16 || n == 17 || n == 18 {
			record(fmt.Sprintf("%d", n))
		}
	}
	be.Use(func(be *BlockEntity) { be.SignFront.Messages[0] = "Edited" })
	record("sign edit")
	const file = "testdata/chunkpacket_golden.txt"
	if os.Getenv("GOLDEN_UPDATE") != "" {
		if err := os.MkdirAll("testdata", 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(file, []byte(out.String()), 0o644); err != nil {
			t.Fatal(err)
		}
		return
	}
	want, err := os.ReadFile(file)
	if err != nil {
		t.Fatal(err)
	}
	if string(want) != out.String() {
		t.Fatalf("packets differ from golden:\n%s\nwant:\n%s", out.String(), want)
	}
}
