package nbt

import (
	"encoding/binary"
	"errors"
	"math"
	"reflect"
	"runtime"
	"slices"
	"strings"
	"testing"
)

// TestDecodeNetworkQuotaReadsWhatDecodeNetworkReads pins that a tag inside
// the quota comes back as DecodeNetwork gives it, every type among it, with
// the bytes after it left alone.
func TestDecodeNetworkQuotaReadsWhatDecodeNetworkReads(t *testing.T) {
	tree := Compound{
		"byte":   Byte(1),
		"short":  Short(2),
		"int":    Int(3),
		"long":   Long(4),
		"float":  Float(0.5),
		"double": Double(math.Pi),
		"string": String("hé\U0001F600"),
		"bytes":  ByteArray{1, 2},
		"ints":   IntArray{1},
		"longs":  LongArray{1, 2},
		"list":   List{Elem: TypeString, Items: []Tag{String("a")}},
		"empty":  List{},
		"nested": Compound{"inner": Compound{}},
	}
	data, err := EncodeNetwork(tree)
	if err != nil {
		t.Fatal(err)
	}
	tag, n, err := DecodeNetworkQuota(append(data, 0xFF), NetworkQuota)
	if err != nil || n != len(data) || !reflect.DeepEqual(tag, tree) {
		t.Errorf("DecodeNetworkQuota = %v, %d, %v; want the tree back and %d bytes", tag, n, err, len(data))
	}
}

func TestDecodeNetworkOrderedQuotaPreservesHistoryAndLimits(t *testing.T) {
	tree, err := ParseOrderedSNBT(`{BB:1,Aa:2,nested:{BB:3,Aa:4}}`)
	if err != nil {
		t.Fatal(err)
	}
	data, err := EncodeNetwork(tree)
	if err != nil {
		t.Fatal(err)
	}
	tag, n, err := DecodeNetworkOrderedQuota(append(data, 0xFF), NetworkQuota)
	if err != nil || n != len(data) || !Equal(tag, tree) {
		t.Fatalf("ordered decode = %v, %d, %v; want the tree and %d bytes", tag, n, err, len(data))
	}
	got := tag.(*OrderedCompound)
	for _, pair := range [][2]CompoundTag{
		{tree, got},
		{tree.Entries()["nested"].(CompoundTag), got.Entries()["nested"].(CompoundTag)},
	} {
		if !slices.Equal(HashOrderedKeys(pair[0]), HashOrderedKeys(pair[1])) {
			t.Fatal("ordered decode lost collision history")
		}
	}
	if _, _, err := DecodeNetworkOrderedQuota(data, 0); !errors.Is(err, ErrOverQuota) {
		t.Fatalf("zero quota: err = %v, want ErrOverQuota", err)
	}
	deep := []byte{byte(TypeList)}
	for range maxDepth + 2 {
		deep = append(deep, byte(TypeList), 0, 0, 0, 1)
	}
	if _, _, err := DecodeNetworkOrderedQuota(deep, math.MaxInt64); err == nil || !strings.Contains(err.Error(), "tags nested more than 512 deep") {
		t.Fatalf("deep ordered decode: err = %v, want depth limit error", err)
	}
}

// stringList is a network list of count one-letter strings: 3 bytes an entry
// on the wire, 4 + 36 + 2 of the accounter's.
func stringList(count int) []byte {
	data := []byte{byte(TypeList), byte(TypeString)}
	data = binary.BigEndian.AppendUint32(data, uint32(count))
	for range count {
		data = append(data, 0, 1, 'a')
	}
	return data
}

// TestDecodeNetworkQuotaRefusesBeforeBuilding pins that the quota is charged
// while reading, not on the finished tree: a list of half a million empty
// compounds, a byte apiece on the wire and 52 of the accounter's units, is
// refused without its half a million maps being built first.
func TestDecodeNetworkQuotaRefusesBeforeBuilding(t *testing.T) {
	const count = 500000
	data := []byte{byte(TypeList), byte(TypeCompound)}
	data = binary.BigEndian.AppendUint32(data, count)
	data = append(data, make([]byte, count)...)

	var before, after runtime.MemStats
	runtime.GC()
	runtime.ReadMemStats(&before)
	_, _, err := DecodeNetworkQuota(data, NetworkQuota)
	runtime.ReadMemStats(&after)
	if !errors.Is(err, ErrOverQuota) {
		t.Errorf("err = %v, want ErrOverQuota", err)
	}
	if n := after.TotalAlloc - before.TotalAlloc; n > 1<<20 {
		t.Errorf("refusing the list allocated %d bytes", n)
	}
}
