package protocol

import (
	"testing"

	"github.com/golem-mc/golem/nbt"
)

// vanilla: ClickEvent.Custom.CODEC (ExtraCodecs.NBT)
func TestPutTextPreservesArbitraryPayloadTags(t *testing.T) {
	tag := nbt.Compound{"text": nbt.String("click"), "click_event": nbt.Compound{
		"action": nbt.String("custom"), "id": nbt.String("test:click"),
		"payload": nbt.Compound{
			"byte": nbt.Byte(-1), "short": nbt.Short(2), "int": nbt.Int(3), "long": nbt.Long(4),
			"float": nbt.Float(0.5), "double": nbt.Double(1.25), "string": nbt.String("data"),
			"bytes": nbt.ByteArray{0, 128, 255}, "ints": nbt.IntArray{1, -1}, "longs": nbt.LongArray{2, -2},
			"mixed": nbt.List{Elem: nbt.TypeCompound, Items: []nbt.Tag{nbt.String("x"), nbt.Int(5)}},
		},
	}}
	var b Buffer
	b.PutText(Text{Raw: tag})
	decoded, n, err := nbt.DecodeNetwork(b.Bytes())
	if err != nil || n != len(b.Bytes()) || !nbt.Equal(decoded, tag) {
		t.Fatalf("payload round trip = %v, %d, %v", decoded, n, err)
	}
}

func TestTextTagKeepsOpaquePayloadHistory(t *testing.T) {
	t.Parallel()
	source, err := nbt.ParseOrderedSNBT(`{text:"x",click_event:{action:"custom",id:"test:action",payload:{bold:1b,text:"opaque"}}}`)
	if err != nil {
		t.Fatal(err)
	}
	tag := TextTag(Text{Raw: source}).(nbt.CompoundTag)
	event, ok := nbt.Get[nbt.CompoundTag](tag.Entries(), "click_event")
	if !ok {
		t.Fatal("click event missing")
	}
	payload, ok := nbt.Get[nbt.CompoundTag](event.Entries(), "payload")
	if !ok {
		t.Fatal("custom payload missing")
	}
	keys := nbt.HashOrderedKeys(payload)
	if len(keys) != 2 || keys[0] != "bold" || keys[1] != "text" {
		t.Errorf("opaque payload keys = %v, want bold before text", keys)
	}
}
