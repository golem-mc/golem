package registry

import (
	"encoding/hex"
	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/nbt"
	"testing"
)

func TestSulfurCubeBucketContentCodecs(t *testing.T) {
	// Vanilla's Set Container Slot payload, after its four-byte header.
	capture, _ := hex.DecodeString("01f40801005001010000")
	r := protocol.NewReader(capture)
	vanilla, ok := r.ReadSlot()
	content, hasContent := vanilla.SulfurCubeContent()
	if name, _ := ItemName(content.ItemID); !ok || !hasContent || name != "minecraft:stone" || r.Remaining() != 0 {
		t.Fatal("vanilla sulfur cube bucket did not decode")
	}

	bucketID, _ := ItemID("minecraft:sulfur_cube_bucket")
	stone, _ := ItemID("minecraft:stone")
	body := protocol.ItemStack{ItemID: stone, Count: 1, CustomName: &protocol.Text{Text: "Inside"}}
	bucket := protocol.ItemStack{ItemID: bucketID, Count: 1}
	bucket.SetBucketEntityData(nbt.Compound{"age": nbt.Int(-1200)})
	bucket.SetSulfurCubeContent(body)
	saved, ok := StoredStack(StackTag(bucket))
	if !ok {
		t.Fatal("bucket NBT did not read")
	}
	for _, untrusted := range []bool{false, true} {
		var b protocol.Buffer
		if untrusted {
			b.PutUntrustedSlot(saved)
		} else {
			b.PutSlot(saved)
		}
		r := protocol.NewReader(b.Bytes())
		var got protocol.ItemStack
		if untrusted {
			got, ok = r.ReadUntrustedSlot()
		} else {
			got, ok = r.ReadSlot()
		}
		if !ok || r.Remaining() != 0 {
			t.Fatalf("slot failed: %v", r.Err())
		}
		content, ok := got.SulfurCubeContent()
		if !ok || content.ItemID != stone || content.Count != 1 || content.CustomName == nil || content.CustomName.Text != "Inside" {
			t.Fatalf("lost content: %+v", content)
		}
		age, _ := nbt.IntField(got.BucketEntityData(), "age")
		if age != -1200 {
			t.Fatalf("lost bucket entity data: %+v", got)
		}
	}
	clone := bucket
	clone.SetSulfurCubeContent(protocol.ItemStack{ItemID: stone, Count: 2})
	content, _ = bucket.SulfurCubeContent()
	if content.Count != 1 {
		t.Fatal("mutated another stack's component")
	}
	for _, raw := range [][]byte{{0, 1, 0, 0}, {1, 0, 0, 0}, {1}} {
		bad := protocol.ItemStack{ItemID: bucketID, Count: 1}
		bad.SetRaw(protocol.RawComponent{Type: protocol.DataComponentSulfurCubeContent, Data: raw})
		var b protocol.Buffer
		b.PutSlot(bad)
		if _, ok := protocol.NewReader(b.Bytes()).ReadSlot(); ok {
			t.Fatal("accepted an empty or truncated template")
		}
	}

}
