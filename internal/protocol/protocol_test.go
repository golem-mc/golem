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

func TestVarIntStream(t *testing.T) {
	for _, want := range []int32{0, 1, 127, 128, 2097151, -1, -2147483648, 2147483647} {
		var b protocol.Buffer
		b.PutVarInt(want)

		got, n, err := protocol.ReadVarInt(bytes.NewReader(b.Bytes()))
		if err != nil {
			t.Fatalf("ReadVarInt(%d): %v", want, err)
		}
		if got != want {
			t.Errorf("ReadVarInt = %d, want %d", got, want)
		}
		if n != b.Len() {
			t.Errorf("ReadVarInt read %d bytes, want %d", n, b.Len())
		}
	}
}

func TestVarIntTooLong(t *testing.T) {
	_, _, err := protocol.ReadVarInt(bytes.NewReader([]byte{0xFF, 0xFF, 0xFF, 0xFF, 0xFF, 0x01}))
	if !errors.Is(err, protocol.ErrVarIntTooLong) {
		t.Errorf("err = %v, want %v", err, protocol.ErrVarIntTooLong)
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

func TestPacketRoundTripEncrypted(t *testing.T) {
	client, server := connPair(t)
	enableEncryption(t, client, server)

	exchange(t, client, server, 0x03, []byte("secret payload"))
	exchange(t, server, client, 0x04, bytes.Repeat([]byte{0xAB}, 1000))
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

// TestReadPacketPayloadOutlivesTheNextRead: the server queues replies made
// of a payload for another goroutine to write, so reading the next packet
// must not overwrite it.
func TestReadPacketPayloadOutlivesTheNextRead(t *testing.T) {
	for _, threshold := range []int{-1, 256} {
		client, server := connPair(t)
		if threshold >= 0 {
			client.EnableCompression(threshold)
			server.EnableCompression(threshold)
		}
		errs := make(chan error, 1)
		go func() {
			if err := client.WritePacket(0x01, []byte("first")); err != nil {
				errs <- err
				return
			}
			errs <- client.WritePacket(0x02, []byte("SECOND"))
		}()

		_ = server.SetReadDeadline(time.Now().Add(5 * time.Second))
		_, first, err := server.ReadPacket()
		if err != nil {
			t.Fatalf("read first packet: %v", err)
		}
		if _, _, err := server.ReadPacket(); err != nil {
			t.Fatalf("read second packet: %v", err)
		}
		if err := <-errs; err != nil {
			t.Fatalf("write packets: %v", err)
		}
		if string(first) != "first" {
			t.Errorf("threshold %d: first payload became %q after the next read", threshold, first)
		}
	}
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

func TestPutAngle(t *testing.T) {
	cases := []struct {
		degrees float32
		want    uint8
	}{
		{0, 0},
		{90, 64},
		{180, 128},
		{-90, 192},
		{360, 0},
		// Angles are not clamped to a turn; the byte wraps.
		{450, 64},
		{-3600 - 90, 192},
	}

	for _, c := range cases {
		var b protocol.Buffer
		b.PutAngle(c.degrees)
		if got := b.Bytes(); len(got) != 1 || got[0] != c.want {
			t.Errorf("PutAngle(%g) = %v, want [%d]", c.degrees, got, c.want)
		}
	}

	// A rotation of any magnitude must convert to whatever the same
	// rotation within a turn would: reducing first keeps the int32
	// conversion defined.
	const huge = float32(1e30)
	var big, reduced protocol.Buffer
	big.PutAngle(huge)
	reduced.PutAngle(float32(math.Mod(float64(huge), 360)))
	if !bytes.Equal(big.Bytes(), reduced.Bytes()) {
		t.Errorf("PutAngle(%g) = %v, want %v", huge, big.Bytes(), reduced.Bytes())
	}
}

func TestPutLpVec3(t *testing.T) {
	// The sample vectors the Minecraft Wiki gives with the LpVec3 definition.
	cases := []struct {
		x, y, z float64
		want    []byte
	}{
		{0, 0, 0, []byte{0x00}},
		{1, 0, -1, []byte{0xF1, 0xFF, 0x00, 0x00, 0xFF, 0xFF}},
		{10, 0.2, -5, []byte{0xF6, 0xFF, 0x40, 0x01, 0x05, 0x1F, 0x02}},
		{123457, 15.071, 0, []byte{0xF5, 0xFF, 0x7F, 0xFF, 0x00, 0x07, 0x90, 0xF1, 0x01}},
	}

	for _, c := range cases {
		var b protocol.Buffer
		b.PutLpVec3(c.x, c.y, c.z)
		if got := b.Bytes(); !bytes.Equal(got, c.want) {
			t.Errorf("PutLpVec3(%g, %g, %g) = % x, want % x", c.x, c.y, c.z, got, c.want)
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

func TestPutTextPlainIsAString(t *testing.T) {
	// Text on its own stays a string tag, both shorter and what the
	// vanilla server sends.
	var b protocol.Buffer
	b.PutText(protocol.Text{Text: "hi"})
	if want := []byte{0x08, 0, 2, 'h', 'i'}; !bytes.Equal(b.Bytes(), want) {
		t.Errorf("PutText = % X, want % X", b.Bytes(), want)
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

func TestReadSlotRefusesComponents(t *testing.T) {
	// A potion as a vanilla server sent it: one component to add (51,
	// potion_contents, holding its own compound). What follows a component is
	// decided by its type, so a reader that does not know the type cannot
	// find the end of the stack and has to give up on the packet.
	data := []byte{0x01, 0xBE, 0x07, 0x01, 0x00, 0x33, 0x0A, 0x00, 0x06, 'p', 'o', 't', 'i', 'o', 'n', 0x00, 0x11, 'm', 'i', 'n', 'e', 'c', 'r', 'a', 'f', 't', ':', 'h', 'e', 'a', 'l', 'i', 'n', 'g'}
	if stack, ok := protocol.NewReader(data).ReadSlot(); ok {
		t.Errorf("ReadSlot = %+v, true; want it to refuse a stack with a component it does not model", stack)
	}

	// A truncated stack is refused rather than read as something else.
	for _, n := range []int{0, 1, 2, 3} {
		if _, ok := protocol.NewReader([]byte{0x05, 0xF4, 0x07, 0x00, 0x00}[:n]).ReadSlot(); ok && n != 0 {
			t.Errorf("ReadSlot accepted %d bytes of a five byte stack", n)
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

func TestSlotCarriesDamage(t *testing.T) {
	// Item 1012 with three damage: count, ID, one component to add (damage,
	// ID 3 here), none to remove, then the wear's VarInt.
	var b protocol.Buffer
	b.PutSlot(protocol.ItemStack{ItemID: 1012, Count: 1, Damage: 3})
	want := []byte{0x01, 0xF4, 0x07, 0x01, 0x00, 0x03, 0x03}
	if !bytes.Equal(b.Bytes(), want) {
		t.Errorf("PutSlot of a worn item = % X, want % X", b.Bytes(), want)
	}

	got, ok := protocol.NewReader(want).ReadSlot()
	if !ok {
		t.Fatal("ReadSlot refused the damage it writes itself")
	}
	if got.Damage != 3 {
		t.Errorf("ReadSlot read the wear as %d, want 3", got.Damage)
	}

	// Wear of zero is the item fresh: no component on the wire at all.
	fresh := protocol.ItemStack{ItemID: 1012, Count: 1}
	b.Reset()
	b.PutSlot(fresh)
	var plain protocol.Buffer
	plain.PutSlot(protocol.ItemStack{ItemID: 1012, Count: 1, Damage: 0})
	if !bytes.Equal(b.Bytes(), plain.Bytes()) {
		t.Errorf("PutSlot of a fresh item = % X, want the plain % X", b.Bytes(), plain.Bytes())
	}

	// A damage removed — the client setting a stack back to its default —
	// is the one removal this server reads rather than refuses.
	b.Reset()
	b.PutVarInt(1) // count
	b.PutVarInt(1012)
	b.PutVarInt(0) // none to add
	b.PutVarInt(1) // one to remove
	b.PutVarInt(protocol.DataComponentDamage)
	stack, ok := protocol.NewReader(b.Bytes()).ReadSlot()
	if !ok || stack.Damage != 0 {
		t.Errorf("ReadSlot of a damage removed = %+v, %v; want the wear read as zero", stack, ok)
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

// TestHashedSlotSpendsAClaimItCannotCompare pins that a claim carrying a
// component the server cannot compare is still read to its end: the reader is
// left after the claim, not inside it, so a packet's trailing-bytes check
// does not mistake the unread fields for extra bytes.
func TestHashedSlotSpendsAClaimItCannotCompare(t *testing.T) {
	var b protocol.Buffer
	b.PutBool(true)
	b.PutVarInt(1012)
	b.PutVarInt(1)
	b.PutVarInt(2) // two added components
	b.PutVarInt(9) // an unmodelled component, with a hash nothing can check
	b.PutI32(0x11223344)
	b.PutVarInt(protocol.DataComponentDamage)
	b.PutI32(0)
	b.PutVarInt(1) // one removed component
	b.PutVarInt(protocol.DataComponentUnbreakable)

	r := protocol.NewReader(b.Bytes())
	if _, ok := r.ReadHashedSlot(); ok {
		t.Error("ReadHashedSlot accepted a claim it cannot compare")
	}
	if r.Remaining() != 0 {
		t.Errorf("ReadHashedSlot left %d bytes unread, want the whole claim", r.Remaining())
	}
}

// TestHashedSlotComponentCountsBoundTheCodec pins the bound on both component
// counts: exactly 256 reads, while 257 - and a negative, which vanilla's map
// and set reject at their capacity allocation - fail the decode.
// vanilla: HashedPatchMap.STREAM_CODEC
func TestHashedSlotComponentCountsBoundTheCodec(t *testing.T) {
	hashed := func(added, removed int) *protocol.Buffer {
		var b protocol.Buffer
		b.PutBool(true)
		b.PutVarInt(1012) // item ID
		b.PutVarInt(1)    // count
		b.PutVarInt(int32(added))
		for range added {
			b.PutVarInt(protocol.DataComponentDamage)
			b.PutI32(0) // the checksum, which this reader does not compare
		}
		b.PutVarInt(int32(removed))
		for range removed {
			b.PutVarInt(protocol.DataComponentDamage)
		}
		return &b
	}

	for _, tc := range []struct {
		name           string
		added, removed int
		ok             bool
	}{
		{"256 added components", 256, 0, true},
		{"256 removed components", 0, 256, true},
		{"257 added components", 257, 0, false},
		{"257 removed components", 0, 257, false},
		{"a negative added count", -1, 0, false},
		{"a negative removed count", 0, -1, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if _, ok := protocol.NewReader(hashed(tc.added, tc.removed).Bytes()).ReadHashedSlot(); ok != tc.ok {
				t.Errorf("ReadHashedSlot ok = %v, want %v", ok, tc.ok)
			}
		})
	}
}

func TestPutTextTooLongStaysReadable(t *testing.T) {
	// An NBT string's length is an unsigned short; one too long goes out
	// empty rather than with a wrapped length, which would leave the client
	// reading the rest of the packet at the wrong offset.
	var b protocol.Buffer
	b.PutText(protocol.Text{Text: strings.Repeat("x", math.MaxUint16+1)})

	if want := []byte{0x08, 0, 0}; !bytes.Equal(b.Bytes(), want) {
		t.Errorf("PutText = % X, want % X (an empty string tag)", b.Bytes(), want)
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

func TestReadStringReplacesInvalidUTF8(t *testing.T) {
	// Malformed bytes come out as one U+FFFD per malformed sequence. An
	// overlong sequence is rejected one byte at a time, an encoded surrogate
	// and a sequence the string ends inside are replaced whole.
	for _, tc := range []struct {
		name string
		raw  string
		want string
	}{
		{"lone continuation byte", "\x80", "\uFFFD"},
		{"overlong sequence", "\xC0\x80", "\uFFFD\uFFFD"},
		{"surrogate", "\xED\xA0\x80", "\uFFFD"},
		{"truncated four byte sequence", "\xF0\x9F\x98", "\uFFFD"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var b protocol.Buffer
			b.PutString(tc.raw)
			if got := protocol.NewReader(b.Bytes()).ReadString(32767); got != tc.want {
				t.Errorf("ReadString = %q, want %q", got, tc.want)
			}
		})
	}
}

func TestReadStringDecodedLengthLimit(t *testing.T) {
	// The byte limit alone admits three times the code units: 48 bytes of
	// ASCII decode to 48 of them, over a String(16)'s 16.
	var b protocol.Buffer
	b.PutString(strings.Repeat("a", 48))
	r := protocol.NewReader(b.Bytes())
	if r.ReadString(16); r.Err() == nil {
		t.Error("expected 48 bytes of ASCII to be rejected against a limit of 16")
	}

	// An emoji is one rune but two code units, so eight fill String(16)
	// and a ninth does not, though both stay under the byte limit.
	emoji := "\U0001F600"
	eight := strings.Repeat(emoji, 8) // 16 code units, 32 bytes
	b.Reset()
	b.PutString(eight)
	if got := protocol.NewReader(b.Bytes()).ReadString(16); got != eight {
		t.Errorf("ReadString = %q, want %q", got, eight)
	}

	b.Reset()
	b.PutString(strings.Repeat(emoji, 9)) // 18 code units, 36 bytes
	r = protocol.NewReader(b.Bytes())
	if r.ReadString(16); r.Err() == nil {
		t.Error("expected nine emoji to be rejected against a limit of 16")
	}
}

func TestEnableEncryptionRejectsWrongKeySize(t *testing.T) {
	// The secret doubles as the feedback register, so only AES-128 works.
	client, _ := connPair(t)
	if err := client.EnableEncryption(make([]byte, 32)); err == nil {
		t.Error("expected a 32-byte shared secret to be rejected")
	}
	if err := client.EnableEncryption(make([]byte, 16)); err != nil {
		t.Errorf("16-byte shared secret: %v", err)
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
