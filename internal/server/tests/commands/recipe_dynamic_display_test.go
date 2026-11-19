package commands

import (
	"fmt"
	"testing"

	"github.com/golem-mc/golem/internal/protocol"
)

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
