package server

import (
	"bufio"
	"bytes"
	"encoding/binary"
	"io"
	"strings"
	"testing"
)

// putRconFrame writes one wire frame: the length of the tail, the id, the
// type, the payload and the terminator pair.
func putRconFrame(b *bytes.Buffer, id, typ int32, payload string) {
	var head [4]byte
	binary.LittleEndian.PutUint32(head[:], uint32(8+len(payload)+2))
	b.Write(head[:])
	binary.LittleEndian.PutUint32(head[:], uint32(id))
	b.Write(head[:])
	binary.LittleEndian.PutUint32(head[:], uint32(typ))
	b.Write(head[:])
	b.WriteString(payload)
	b.Write([]byte{0, 0})
}

// readOneFrame reads one frame out of a buffer the writer filled.
func readOneFrame(t *testing.T, b *bytes.Reader) (id, typ int32, payload string) {
	t.Helper()
	var head [4]byte
	if _, err := io.ReadFull(b, head[:]); err != nil {
		t.Fatal(err)
	}
	length := int(binary.LittleEndian.Uint32(head[:]))
	frame := make([]byte, length)
	if _, err := io.ReadFull(b, frame); err != nil {
		t.Fatal(err)
	}
	return int32(binary.LittleEndian.Uint32(frame[0:4])),
		int32(binary.LittleEndian.Uint32(frame[4:8])),
		string(frame[8 : len(frame)-2])
}

// TestWriteRconOutputChunksAndAnswers checks the two halves of a command's
// answer: a long output leaves in packets of at most 4096 characters, and a
// command that said nothing is still one empty answer, not silence.
func TestWriteRconOutputChunksAndAnswers(t *testing.T) {
	t.Parallel()
	long := strings.Repeat("word ", 1100) // 5500 characters
	var out bytes.Buffer
	if err := writeRconOutput(&out, 7, long); err != nil {
		t.Fatalf("write rcon output: %v", err)
	}
	r := bytes.NewReader(out.Bytes())
	id, typ, first := readOneFrame(t, r)
	if id != 7 || typ != rconTypeOutput {
		t.Fatalf("first packet = id %d type %d, want id 7 type 0", id, typ)
	}
	if first != long[:rconMaxOutput] {
		t.Errorf("first chunk carries %d characters, want the first %d",
			len([]rune(first)), rconMaxOutput)
	}
	_, _, second := readOneFrame(t, r)
	if second != long[rconMaxOutput:] {
		t.Errorf("second chunk = %q, want the remaining %d", second, len(long)-rconMaxOutput)
	}
	if r.Len() != 0 {
		t.Errorf("%d bytes left over after the last chunk", r.Len())
	}

	var empty bytes.Buffer
	if err := writeRconOutput(&empty, 8, ""); err != nil {
		t.Fatalf("write rcon output: %v", err)
	}
	id, typ, payload := readOneFrame(t, bytes.NewReader(empty.Bytes()))
	if id != 8 || typ != rconTypeOutput || payload != "" {
		t.Errorf("empty output = id %d type %d payload %q, want one empty answer", id, typ, payload)
	}
}

// TestReadRconRequestFramesAndBounds checks the request reader: a well-formed
// frame reads back its fields, an embedded null ends the payload where the
// frame goes on, and a length outside the window vanilla reads requests in is
// refused rather than read.
func TestReadRconRequestFramesAndBounds(t *testing.T) {
	t.Parallel()
	var in bytes.Buffer
	putRconFrame(&in, 3, rconTypeAuth, "sesame")
	putRconFrame(&in, 4, rconTypeCommand, "list\x00never mind")
	r := bufio.NewReader(&in)
	req, ok := readRconRequest(r)
	if !ok || req.id != 3 || req.typ != rconTypeAuth || req.payload != "sesame" {
		t.Errorf("request = %+v ok %v, want the sesame login", req, ok)
	}
	req, ok = readRconRequest(r)
	if !ok || req.id != 4 || req.typ != rconTypeCommand || req.payload != "list" {
		t.Errorf("request = %+v ok %v, want the list command cut at its null", req, ok)
	}

	for name, length := range map[string]int32{"too small": rconMinRequest - 1, "too large": rconMaxRequest + 1} {
		var head [4]byte
		binary.LittleEndian.PutUint32(head[:], uint32(length))
		if _, ok := readRconRequest(bufio.NewReader(bytes.NewReader(head[:]))); ok {
			t.Errorf("a length %d (%s) was accepted", length, name)
		}
	}
}
