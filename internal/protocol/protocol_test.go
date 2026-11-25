package protocol_test

import (
	"bytes"
	"compress/zlib"
	"crypto/rand"
	"errors"
	"io"
	"math"
	"net"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/golem-mc/golem/internal/protocol"
	"github.com/google/uuid"
)

// enableEncryption generates a shared secret and switches both ends of a
// connection pair over to the encrypted packet format.
func enableEncryption(t *testing.T, client, server *protocol.Conn) {
	t.Helper()
	secret := make([]byte, 16)
	rand.Read(secret)
	if err := client.EnableEncryption(secret); err != nil {
		t.Fatalf("client encryption: %v", err)
	}
	if err := server.EnableEncryption(secret); err != nil {
		t.Fatalf("server encryption: %v", err)
	}
}

func TestBufferRoundTrip(t *testing.T) {
	var b protocol.Buffer
	b.PutBool(true)
	b.PutU8(200)
	b.PutI16(-1234)
	b.PutI32(-70000)
	b.PutI64(1 << 40)
	b.PutF32(0.5)
	b.PutF64(-12.25)
	b.PutVarInt(-1)
	b.PutString("hello wörld")
	b.PutBytes([]byte{1, 2, 3})

	r := protocol.NewReader(b.Bytes())
	if got := r.ReadBool(); !got {
		t.Errorf("bool = %v, want true", got)
	}
	if got := r.ReadU8(); got != 200 {
		t.Errorf("u8 = %d, want 200", got)
	}
	if got := r.ReadI16(); got != -1234 {
		t.Errorf("i16 = %d, want -1234", got)
	}
	if got := r.ReadI32(); got != -70000 {
		t.Errorf("i32 = %d, want -70000", got)
	}
	if got := r.ReadI64(); got != 1<<40 {
		t.Errorf("i64 = %d, want %d", got, 1<<40)
	}
	if got := r.ReadF32(); got != 0.5 {
		t.Errorf("f32 = %v, want 0.5", got)
	}
	if got := r.ReadF64(); got != -12.25 {
		t.Errorf("f64 = %v, want -12.25", got)
	}
	if got := r.ReadVarInt(); got != -1 {
		t.Errorf("varint = %d, want -1", got)
	}
	if got := r.ReadString(32767); got != "hello wörld" {
		t.Errorf("string = %q, want %q", got, "hello wörld")
	}
	if got := r.ReadBytes(16); !bytes.Equal(got, []byte{1, 2, 3}) {
		t.Errorf("bytes = %v, want [1 2 3]", got)
	}
	if err := r.Err(); err != nil {
		t.Fatalf("read: %v", err)
	}
	if r.Remaining() != 0 {
		t.Errorf("%d bytes left over", r.Remaining())
	}
}

func TestReaderShortPacket(t *testing.T) {
	var b protocol.Buffer
	b.PutVarInt(5)

	r := protocol.NewReader(b.Bytes())
	r.ReadVarInt()
	r.ReadI64()
	if !errors.Is(r.Err(), protocol.ErrShortPacket) {
		t.Errorf("err = %v, want %v", r.Err(), protocol.ErrShortPacket)
	}
	// Once a read fails the rest yields zero values instead of panicking.
	if got := r.ReadString(16); got != "" {
		t.Errorf("string after failure = %q, want empty", got)
	}
}

func TestVarIntTooLong(t *testing.T) {
	_, _, err := protocol.ReadVarInt(bytes.NewReader([]byte{0xFF, 0xFF, 0xFF, 0xFF, 0xFF, 0x01}))
	if !errors.Is(err, protocol.ErrVarIntTooLong) {
		t.Errorf("err = %v, want %v", err, protocol.ErrVarIntTooLong)
	}
}

// onlyReader hides every method but Read, forcing the io.Reader fallback.
type onlyReader struct{ r io.Reader }

func (o onlyReader) Read(p []byte) (int, error) { return o.r.Read(p) }

func TestReadVarIntByteReaderAndReader(t *testing.T) {
	cases := []struct {
		name     string
		in       []byte
		want     int32
		wantN    int
		wantErr  error
		wantLeft int
	}{
		{"one byte", []byte{0x05, 0xAA}, 5, 1, nil, 1},
		{"multi byte", []byte{0xAC, 0x02, 0xAA}, 300, 2, nil, 1},
		{"five bytes", []byte{0xFF, 0xFF, 0xFF, 0xFF, 0x07, 0xAA}, math.MaxInt32, 5, nil, 1},
		{"empty", nil, 0, 0, io.EOF, 0},
		{"early EOF", []byte{0x80, 0x80}, 0, 2, io.EOF, 0},
		{"too long", []byte{0xFF, 0xFF, 0xFF, 0xFF, 0xFF, 0x01}, 0, 5, protocol.ErrVarIntTooLong, 1},
	}
	wrap := map[string]func([]byte) (io.Reader, func() int){
		"ByteReader": func(b []byte) (io.Reader, func() int) {
			r := bytes.NewReader(b)
			return r, r.Len
		},
		"Reader": func(b []byte) (io.Reader, func() int) {
			r := bytes.NewReader(b)
			return onlyReader{r}, r.Len
		},
	}
	for kind, mk := range wrap {
		for _, c := range cases {
			t.Run(kind+"/"+c.name, func(t *testing.T) {
				r, left := mk(c.in)
				got, n, err := protocol.ReadVarInt(r)
				if got != c.want || n != c.wantN || !errors.Is(err, c.wantErr) || left() != c.wantLeft {
					t.Errorf("got (%d, %d, %v) with %d left, want (%d, %d, %v) with %d left",
						got, n, err, left(), c.want, c.wantN, c.wantErr, c.wantLeft)
				}
			})
		}
	}
}

// connPair returns two connected protocol connections.
func connPair(t *testing.T) (*protocol.Conn, *protocol.Conn) {
	t.Helper()
	a, b := net.Pipe()
	t.Cleanup(func() { a.Close(); b.Close() })
	return protocol.NewConn(a), protocol.NewConn(b)
}

// exchange writes a packet on one connection and reads it back on the other.
func exchange(t *testing.T, from, to *protocol.Conn, id int32, payload []byte) {
	t.Helper()
	errs := make(chan error, 1)
	go func() { errs <- from.WritePacket(id, payload) }()

	_ = to.SetReadDeadline(time.Now().Add(5 * time.Second))
	gotID, gotData, err := to.ReadPacket()
	if err != nil {
		t.Fatalf("read packet: %v", err)
	}
	if err := <-errs; err != nil {
		t.Fatalf("write packet: %v", err)
	}
	if gotID != id {
		t.Errorf("packet ID = 0x%02X, want 0x%02X", gotID, id)
	}
	if !bytes.Equal(gotData, payload) {
		t.Errorf("payload = %d bytes, want %d", len(gotData), len(payload))
	}
}

func TestPacketRoundTrip(t *testing.T) {
	client, server := connPair(t)
	exchange(t, client, server, 0x12, []byte("hello"))
	exchange(t, server, client, 0x34, nil)
}

func TestPacketRoundTripCompressed(t *testing.T) {
	client, server := connPair(t)
	client.EnableCompression(16)
	server.EnableCompression(16)

	// Below the threshold packets go verbatim, above it deflated; both
	// have to survive the trip.
	exchange(t, client, server, 0x01, []byte("short"))
	exchange(t, client, server, 0x02, bytes.Repeat([]byte("compress me "), 100))
}

func TestPacketRoundTripEncryptedAndCompressed(t *testing.T) {
	client, server := connPair(t)
	enableEncryption(t, client, server)
	client.EnableCompression(16)
	server.EnableCompression(16)

	exchange(t, client, server, 0x05, bytes.Repeat([]byte("data"), 500))
}

// TestWritePacketFrames checks the bytes WritePacket puts on the wire against
// the frames built by hand from the protocol's layout: uncompressed, below the
// compression threshold, and deflated. The packet ID and the lengths take more
// than one VarInt byte, so a prefix written at the wrong offset shows.
func TestWritePacketFrames(t *testing.T) {
	const id = 300
	payload := bytes.Repeat([]byte("frame "), 50)
	var body protocol.Buffer
	body.PutVarInt(id)
	body.PutRaw(payload)

	read := func(t *testing.T, threshold int) []byte {
		t.Helper()
		a, b := net.Pipe()
		t.Cleanup(func() { a.Close(); b.Close() })
		conn := protocol.NewConn(a)
		if threshold >= 0 {
			conn.EnableCompression(threshold)
		}
		errs := make(chan error, 1)
		go func() { errs <- conn.WritePacket(id, payload) }()
		_ = b.SetReadDeadline(time.Now().Add(5 * time.Second))
		n, _, err := protocol.ReadVarInt(b)
		if err != nil {
			t.Fatalf("read length: %v", err)
		}
		frame := make([]byte, n)
		if _, err := io.ReadFull(b, frame); err != nil {
			t.Fatalf("read frame: %v", err)
		}
		if err := <-errs; err != nil {
			t.Fatalf("write packet: %v", err)
		}
		return frame
	}

	t.Run("uncompressed", func(t *testing.T) {
		if got := read(t, -1); !bytes.Equal(got, body.Bytes()) {
			t.Errorf("frame = % x, want % x", got, body.Bytes())
		}
	})
	t.Run("below threshold", func(t *testing.T) {
		want := append([]byte{0}, body.Bytes()...)
		if got := read(t, body.Len()+1); !bytes.Equal(got, want) {
			t.Errorf("frame = % x, want % x", got, want)
		}
	})
	t.Run("deflated", func(t *testing.T) {
		frame := read(t, 16)
		r := bytes.NewReader(frame)
		size, _, err := protocol.ReadVarInt(r)
		if err != nil {
			t.Fatalf("read data length: %v", err)
		}
		if int(size) != body.Len() {
			t.Errorf("data length = %d, want %d", size, body.Len())
		}
		zr, err := zlib.NewReader(r)
		if err != nil {
			t.Fatalf("open deflate stream: %v", err)
		}
		got, err := io.ReadAll(zr)
		if err != nil {
			t.Fatalf("inflate: %v", err)
		}
		if !bytes.Equal(got, body.Bytes()) {
			t.Errorf("inflated %d bytes, want the %d of the body", len(got), body.Len())
		}
	})
}

func TestPutPosition(t *testing.T) {
	var b protocol.Buffer
	b.PutPosition(1, 2, 3)

	// x in the top 26 bits, then z, then y in the low 12.
	const want = int64(1)<<38 | int64(3)<<12 | 2
	if got := protocol.NewReader(b.Bytes()).ReadI64(); got != want {
		t.Errorf("position = %#x, want %#x", got, want)
	}
}

func TestReadPosition(t *testing.T) {
	cases := []struct {
		x, y, z int32
	}{
		{0, 0, 0},
		{1, 2, 3},
		{-1, -1, -1}, // a y of -1 is the bottom of the world, not 0xFFF
		{30000000, 2047, 30000000},
		{-30000000, -2048, -30000000},
	}
	for _, c := range cases {
		var b protocol.Buffer
		b.PutPosition(c.x, c.y, c.z)
		x, y, z := protocol.NewReader(b.Bytes()).ReadPosition()
		if x != c.x || y != c.y || z != c.z {
			t.Errorf("ReadPosition(%d, %d, %d) = (%d, %d, %d)", c.x, c.y, c.z, x, y, z)
		}
	}
}

func TestLpVec3RoundTrip(t *testing.T) {
	cases := [][3]float64{
		{0, 0, 0},
		{1, 0, -1},
		{10, 0.2, -5},
		{-0.5, 0.25, 0.125},
		{123457, 15.071, 0},
	}

	for _, c := range cases {
		var b protocol.Buffer
		b.PutLpVec3(c[0], c[1], c[2])
		r := protocol.NewReader(b.Bytes())
		x, y, z := r.ReadLpVec3()
		if r.Remaining() != 0 || r.Err() != nil {
			t.Errorf("reading back %v left %d bytes, err %v", c, r.Remaining(), r.Err())
		}
		// The magnitude sets the quantization step, so the error that
		// comes back is relative to the largest component.
		magnitude := math.Max(math.Abs(c[0]), math.Max(math.Abs(c[1]), math.Abs(c[2])))
		tolerance := math.Ceil(magnitude) * 2 / 32766
		if math.Abs(x-c[0]) > tolerance || math.Abs(y-c[1]) > tolerance || math.Abs(z-c[2]) > tolerance {
			t.Errorf("LpVec3 round trip of %v = (%g, %g, %g), tolerance %g", c, x, y, z, tolerance)
		}
	}
}

func TestReadPacketRejectsHugePacket(t *testing.T) {
	raw, peer := net.Pipe()
	defer raw.Close()
	defer peer.Close()
	server := protocol.NewConn(raw)

	go func() {
		// A length no real packet ever has, written without WritePacket's
		// framing.
		var b protocol.Buffer
		b.PutVarInt(1 << 30)
		_, _ = peer.Write(b.Bytes())
	}()

	_ = server.SetReadDeadline(time.Now().Add(5 * time.Second))
	_, _, err := server.ReadPacket()
	if err == nil {
		t.Fatal("expected an error for an oversized packet")
	}
	if errors.Is(err, io.EOF) || errors.Is(err, os.ErrDeadlineExceeded) {
		t.Fatalf("expected the length to be rejected, got %v", err)
	}
}

func TestOfflineUUID(t *testing.T) {
	// The UUID the vanilla server gives "Notch" in offline mode.
	const want = "b50ad385-829d-3141-a216-7e7d7539ba7f"
	if got := protocol.OfflineUUID("Notch").String(); got != want {
		t.Errorf("OfflineUUID = %s, want %s", got, want)
	}

	// uuid.NewMD5 hashes the namespace ahead of the name and lands
	// elsewhere; guard against anyone "simplifying" OfflineUUID into it.
	if got := uuid.NewMD5(uuid.Nil, []byte("OfflinePlayer:Notch")).String(); got == want {
		t.Error("uuid.NewMD5 now matches nameUUIDFromBytes; OfflineUUID could be simplified")
	}
}

func TestPutTextComponent(t *testing.T) {
	// NBT stores strings as Java's modified UTF-8: NUL written long-hand,
	// anything above the basic plane as its two surrogates.
	for _, tc := range []struct {
		name string
		text string
		want []byte
	}{
		{"ascii", "hi", []byte{0x08, 0, 2, 'h', 'i'}},
		{"nul", "\x00", []byte{0x08, 0, 2, 0xC0, 0x80}},
		{"two byte", "ä", []byte{0x08, 0, 2, 0xC3, 0xA4}},
		{"three byte", "€", []byte{0x08, 0, 3, 0xE2, 0x82, 0xAC}},
		{"supplementary", "\U0001F600", []byte{0x08, 0, 6, 0xED, 0xA0, 0xBD, 0xED, 0xB8, 0x80}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var b protocol.Buffer
			b.PutTextComponent(tc.text)
			if got := b.Bytes(); !bytes.Equal(got, tc.want) {
				t.Errorf("PutTextComponent(%q) = % X, want % X", tc.text, got, tc.want)
			}
		})
	}
}

func TestPutTextTranslated(t *testing.T) {
	// The bytes of the message a vanilla server announces a player
	// with, minus the click and hover behaviour; read off with tools/probe.
	var b protocol.Buffer
	b.PutText(protocol.Text{
		Translate: "multiplayer.player.joined",
		With:      []protocol.Text{{Text: "Joiner"}},
		Color:     protocol.ColorYellow,
	})

	want := []byte{0x0A} // compound, unnamed as the root of network NBT
	want = append(want, 0x08, 0, 9)
	want = append(want, "translate"...)
	want = append(want, 0, 25)
	want = append(want, "multiplayer.player.joined"...)
	want = append(want, 0x09, 0, 4)
	want = append(want, "with"...)
	// A with-list whose every argument is plain text travels as a string
	// list, as the vanilla server was seen to send for its own join notice.
	want = append(want, 0x08, 0, 0, 0, 1)
	want = append(want, 0, 6)
	want = append(want, "Joiner"...)
	want = append(want, 0x08, 0, 5)
	want = append(want, "color"...)
	want = append(want, 0, 6)
	want = append(want, "yellow"...)
	want = append(want, 0x00)

	if got := b.Bytes(); !bytes.Equal(got, want) {
		t.Errorf("PutText =\n% X\nwant\n% X", got, want)
	}
}

func TestPutTextStyled(t *testing.T) {
	// A component carrying italic or underline is a compound, as vanilla
	// writes the "<--[HERE]" marker and a command error's underlined
	// context; each decoration is a named byte tag set to 1.
	for _, tc := range []struct {
		name string
		text protocol.Text
		want []byte
	}{
		{
			"italic",
			protocol.Text{Translate: "command.context.here", Color: protocol.ColorRed, Italic: true},
			bytes.Join([][]byte{
				{0x0A}, // compound
				{0x08, 0, 9, 't', 'r', 'a', 'n', 's', 'l', 'a', 't', 'e'},
				{0, 20}, []byte("command.context.here"),
				{0x08, 0, 5, 'c', 'o', 'l', 'o', 'r'}, {0, 3}, []byte("red"),
				{0x01, 0, 6, 'i', 't', 'a', 'l', 'i', 'c', 0x01},
				{0x00},
			}, nil),
		},
		{
			"underline",
			protocol.Text{Text: "foo", Color: protocol.ColorRed, Underline: true},
			bytes.Join([][]byte{
				{0x0A},
				{0x08, 0, 4, 't', 'e', 'x', 't'}, {0, 3}, []byte("foo"),
				{0x08, 0, 5, 'c', 'o', 'l', 'o', 'r'}, {0, 3}, []byte("red"),
				{0x01, 0, 10, 'u', 'n', 'd', 'e', 'r', 'l', 'i', 'n', 'e', 'd', 0x01},
				{0x00},
			}, nil),
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var b protocol.Buffer
			b.PutText(tc.text)
			if got := b.Bytes(); !bytes.Equal(got, tc.want) {
				t.Errorf("PutText = % X, want % X", got, tc.want)
			}
		})
	}
}

func TestSlotRoundTrip(t *testing.T) {
	// Read off a vanilla server with tools/probe: giving a player five
	// diamonds (item 1012) put this in slot 36. A plain item is a count, an
	// ID and two zero component counts.
	var b protocol.Buffer
	b.PutSlot(protocol.ItemStack{ItemID: 1012, Count: 5})
	if want := []byte{0x05, 0xF4, 0x07, 0x00, 0x00}; !bytes.Equal(b.Bytes(), want) {
		t.Errorf("PutSlot = % X, want % X", b.Bytes(), want)
	}

	// An empty slot is a lone zero: the count says whether anything follows.
	b.Reset()
	b.PutSlot(protocol.ItemStack{})
	if want := []byte{0x00}; !bytes.Equal(b.Bytes(), want) {
		t.Errorf("PutSlot(empty) = % X, want % X", b.Bytes(), want)
	}

	for _, stack := range []protocol.ItemStack{
		{},
		{ItemID: 1012, Count: 5},
		{ItemID: 1, Count: 64},
		{ItemID: 1657, Count: 1},
	} {
		var b protocol.Buffer
		b.PutSlot(stack)
		got, ok := protocol.NewReader(b.Bytes()).ReadSlot()
		if !ok {
			t.Errorf("ReadSlot refused what PutSlot wrote for %+v", stack)
			continue
		}
		if stack.Empty() {
			if !got.Empty() {
				t.Errorf("ReadSlot = %+v, want an empty slot", got)
			}
		} else if !got.Same(stack) || got.Count != stack.Count {
			t.Errorf("ReadSlot = %+v, want %+v", got, stack)
		}
	}
}

func TestSlotCarriesComponents(t *testing.T) {
	// Every component the stack model keeps round-trips. The order the
	// components travel in is the registry's own numbering: wear first,
	// stored enchantments last.
	name := protocol.Text{Text: "Sting"}
	for _, stack := range []protocol.ItemStack{
		{ItemID: 1012, Count: 1, Damage: 7},
		{ItemID: 1012, Count: 1, RepairCost: 3},
		{ItemID: 1012, Count: 1, GlintOverride: protocol.GlintShown},
		{ItemID: 1012, Count: 1, GlintOverride: protocol.GlintHidden},
		{ItemID: 1012, Count: 1, Unbreakable: true},
		{ItemID: 1050, Count: 1, CustomName: &name},
		{ItemID: 1050, Count: 1, Enchantments: map[int32]int32{33: 5}},
		{ItemID: 1395, Count: 1, StoredEnchantments: map[int32]int32{33: 3}},
		{ItemID: 1050, Count: 1, Damage: 10, Enchantments: map[int32]int32{33: 5}, RepairCost: 2},
	} {
		var b protocol.Buffer
		b.PutSlot(stack)
		got, ok := protocol.NewReader(b.Bytes()).ReadSlot()
		if !ok {
			t.Errorf("ReadSlot refused what PutSlot wrote for %+v", stack)
			continue
		}
		if !got.Same(stack) || got.Count != stack.Count {
			t.Errorf("ReadSlot = %+v, want %+v", got, stack)
		}
	}

	// An enchantment the registry has no ID for is refused: the stack it
	// rides on cannot be carried anywhere.
	var b protocol.Buffer
	b.PutVarInt(1) // count
	b.PutVarInt(1012)
	b.PutVarInt(1) // one to add
	b.PutVarInt(protocol.DataComponentEnchantments)
	b.PutVarInt(1) // one entry
	b.PutVarInt(300)
	b.PutVarInt(5)
	if stack, ok := protocol.NewReader(b.Bytes()).ReadSlot(); ok {
		t.Errorf("ReadSlot = %+v, true; want it to refuse an enchantment it has no ID for", stack)
	}

	// A level outside what the game's own codec reads back is refused too.
	for _, level := range []int32{0, 256} {
		b.Reset()
		b.PutVarInt(1)
		b.PutVarInt(1012)
		b.PutVarInt(1)
		b.PutVarInt(protocol.DataComponentEnchantments)
		b.PutVarInt(1)
		b.PutVarInt(33)
		b.PutVarInt(level)
		if stack, ok := protocol.NewReader(b.Bytes()).ReadSlot(); ok {
			t.Errorf("ReadSlot = %+v, true; want it to refuse an enchantment at level %d", stack, level)
		}
	}
}

func TestHashedSlotMatchesDamage(t *testing.T) {
	// The damage checksum is the codec-hash the hashed slot carries, as a
	// vanilla server confirmed on a click; the wear-three value is
	// pinned the way the wire's own bytes are.
	checksum := func(damage int32) int32 {
		got, ok := protocol.ComponentChecksumOf(protocol.ItemStack{Damage: damage}, protocol.DataComponentDamage)
		if !ok {
			t.Fatal("the damage checksum is not computable")
		}
		return got
	}

	// A hashed claim of a stack with damage: has item, ID, count, the
	// damage component with its checksum, none to remove.
	var b protocol.Buffer
	b.PutBool(true)
	b.PutVarInt(1012)
	b.PutVarInt(1)
	b.PutVarInt(1)
	b.PutVarInt(protocol.DataComponentDamage)
	b.PutI32(checksum(3))
	b.PutVarInt(0)
	claim, ok := protocol.NewReader(b.Bytes()).ReadHashedSlot()
	if !ok {
		t.Fatal("ReadHashedSlot refused a damage checksum")
	}
	if !claim.Matches(protocol.ItemStack{ItemID: 1012, Count: 1, Damage: 3}) {
		t.Error("a claim of three damage fails to match the stack carrying three")
	}
	if claim.Matches(protocol.ItemStack{ItemID: 1012, Count: 1, Damage: 2}) {
		t.Error("a claim of three damage matches a stack carrying two")
	}
	if claim.Matches(protocol.ItemStack{ItemID: 1012, Count: 2, Damage: 3}) {
		t.Error("a claim matches a stack of another count")
	}

	// A claim of a fresh stack — no components — matches only the stack
	// with no wear.
	b.Reset()
	b.PutBool(true)
	b.PutVarInt(1012)
	b.PutVarInt(1)
	b.PutVarInt(0)
	b.PutVarInt(0)
	claim, ok = protocol.NewReader(b.Bytes()).ReadHashedSlot()
	if !ok {
		t.Fatal("ReadHashedSlot refused a plain claim")
	}
	if !claim.Matches(protocol.ItemStack{ItemID: 1012, Count: 1}) {
		t.Error("a plain claim fails to match a fresh stack")
	}
	if claim.Matches(protocol.ItemStack{ItemID: 1012, Count: 1, Damage: 3}) {
		t.Error("a plain claim matches a worn stack")
	}

	// A claim carrying an unmodelled component is not comparable at all.
	b.Reset()
	b.PutBool(true)
	b.PutVarInt(1012)
	b.PutVarInt(1)
	b.PutVarInt(1)
	b.PutVarInt(51) // potion_contents, whose checksum says nothing this server can verify
	b.PutI32(0)
	b.PutVarInt(0)
	if _, ok := protocol.NewReader(b.Bytes()).ReadHashedSlot(); ok {
		t.Error("ReadHashedSlot accepted a checksum it cannot compare")
	}
}

func TestReadStringLimitIsInCodeUnits(t *testing.T) {
	// A String(n) may take up to three bytes per code unit.
	text := strings.Repeat("€", 16) // 16 code units, 48 bytes
	var b protocol.Buffer
	b.PutString(text)

	if got := protocol.NewReader(b.Bytes()).ReadString(16); got != text {
		t.Errorf("ReadString = %q, want %q", got, text)
	}

	var tooLong protocol.Buffer
	tooLong.PutString(strings.Repeat("a", 49))
	r := protocol.NewReader(tooLong.Bytes())
	if r.ReadString(16); r.Err() == nil {
		t.Error("expected a string over the byte limit to be rejected")
	}
}

func TestUUIDRoundTrip(t *testing.T) {
	want := protocol.OfflineUUID("Testificate")

	var b protocol.Buffer
	b.PutUUID(want)
	if b.Len() != 16 {
		t.Errorf("encoded UUID is %d bytes, want 16", b.Len())
	}
	if got := protocol.NewReader(b.Bytes()).ReadUUID(); got != want {
		t.Errorf("round trip = %s, want %s", got, want)
	}
}
