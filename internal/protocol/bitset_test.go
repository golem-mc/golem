package protocol

import (
	"bytes"
	"testing"
)

func TestPutBitSet(t *testing.T) {
	for _, c := range []struct {
		in   []uint64
		want []byte
	}{
		{nil, []byte{0}},
		{[]uint64{0}, []byte{0}},
		{[]uint64{1}, []byte{1, 1}},
		{[]uint64{1 << 8}, []byte{2, 0, 1}},
		{[]uint64{0, 1}, []byte{9, 0, 0, 0, 0, 0, 0, 0, 0, 1}},
	} {
		var b Buffer
		b.PutBitSet(c.in)
		if !bytes.Equal(b.Bytes(), c.want) {
			t.Errorf("PutBitSet(%v) = % x, want % x", c.in, b.Bytes(), c.want)
		}
	}
}

func TestReadBitSet(t *testing.T) {
	var b Buffer
	b.PutBitSet([]uint64{0x3ffffff})
	r := NewReader(b.Bytes())
	if got := r.ReadBitSet(); got != 0x3ffffff || r.Err() != nil {
		t.Errorf("ReadBitSet = %x, %v", got, r.Err())
	}
}
