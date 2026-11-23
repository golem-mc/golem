package protocol

import (
	"bytes"
	"testing"
)

func TestMoveEntityEncodings(t *testing.T) {
	var b Buffer
	PutMoveEntityPos(&b, 5, 1, -1, 256, 0, true)
	if want := []byte{5, 1, 0, 1, 0xff, 0xff, 1, 0}; !bytes.Equal(b.Bytes(), want) {
		t.Errorf("Pos = % x, want % x", b.Bytes(), want)
	}
	b = Buffer{}
	PutMoveEntityPosRot(&b, 5, 1, 0, 0, 0x40, 0x10, 0, false)
	if want := []byte{5, 0, 0, 1, 0, 0, 0, 0, 0x40, 0x10}; !bytes.Equal(b.Bytes(), want) {
		t.Errorf("PosRot = % x, want % x", b.Bytes(), want)
	}
	b = Buffer{}
	PutMoveEntityPos(&b, 5, 1, -1, 256, 3, true)
	if want := []byte{5, 3, 3, 0, 1, 0xff, 0xff, 1, 0}; !bytes.Equal(b.Bytes(), want) {
		t.Errorf("stepped Pos = % x, want % x", b.Bytes(), want)
	}
	b = Buffer{}
	PutMoveEntityPosRot(&b, 5, 1, 0, 0, 0x40, 0x10, 2, false)
	if want := []byte{5, 2, 2, 0, 1, 0, 0, 0, 0, 0x40, 0x10}; !bytes.Equal(b.Bytes(), want) {
		t.Errorf("stepped PosRot = % x, want % x", b.Bytes(), want)
	}
	b = Buffer{}
	PutMoveEntityRot(&b, 300, 0x40, 0x10, true)
	if want := []byte{0xac, 0x02, 1, 0x40, 0x10}; !bytes.Equal(b.Bytes(), want) {
		t.Errorf("Rot = % x, want % x", b.Bytes(), want)
	}
}

func TestEntityPositionSyncEncoding(t *testing.T) {
	var b Buffer
	PutEntityPositionSync(&b, 7, 1, 0, -2, 1, 0, true)
	want := []byte{7, 0,
		0x3f, 0xf0, 0, 0, 0, 0, 0, 0,
		0, 0, 0, 0, 0, 0, 0, 0,
		0xc0, 0, 0, 0, 0, 0, 0, 0,
		0x3f, 0x80, 0, 0,
		0, 0, 0, 0,
		1}
	if !bytes.Equal(b.Bytes(), want) {
		t.Errorf("sync = % x, want % x", b.Bytes(), want)
	}
	b = Buffer{}
	PutEntityPositionSyncStepped(&b, 7, 1, 0, -2, 3, 1, 0, true)
	want = []byte{7, 1, 1,
		0x3f, 0xf0, 0, 0, 0, 0, 0, 0,
		0, 0, 0, 0, 0, 0, 0, 0,
		0xc0, 0, 0, 0, 0, 0, 0, 0,
		3,
		0x3f, 0x80, 0, 0,
		0, 0, 0, 0,
		1}
	if !bytes.Equal(b.Bytes(), want) {
		t.Errorf("stepped sync = % x, want % x", b.Bytes(), want)
	}
}
