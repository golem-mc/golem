package protocol

import (
	"bytes"
	"sync"
	"testing"

	"github.com/klauspost/compress/zlib"
)

func compressedFrame(t testing.TB, payload []byte) []byte {
	t.Helper()
	var frame Buffer
	frame.PutVarInt(int32(len(payload)))
	zw := zlib.NewWriter(&frame)
	if _, err := zw.Write(payload); err != nil {
		t.Fatal(err)
	}
	if err := zw.Close(); err != nil {
		t.Fatal(err)
	}
	return frame.Bytes()
}

func TestDecompressReusesReaders(t *testing.T) {
	t.Parallel()
	var frames, payloads [][]byte
	for i, size := range []int{64, 4096, 512, 1, 65536, 70 << 10, 17} {
		payload := make([]byte, size)
		for j := range payload {
			payload[j] = byte(i + j*7 + j/128)
		}
		payloads = append(payloads, payload)
		frames = append(frames, compressedFrame(t, payload))
	}
	for range 5 {
		for i, frame := range frames {
			got, err := decompress(frame, 0)
			if err != nil || !bytes.Equal(got, payloads[i]) {
				t.Fatalf("frame %d: decompress = %d bytes, %v; want %d matching bytes", i, len(got), err, len(payloads[i]))
			}
		}
	}
}

func TestDecompressConcurrentReuse(t *testing.T) {
	t.Parallel()
	var frames, payloads [][]byte
	for i := range 8 {
		payload := bytes.Repeat([]byte{byte(i), byte(i + 1), byte(i + 2)}, 100+i*1000)
		payloads = append(payloads, payload)
		frames = append(frames, compressedFrame(t, payload))
	}
	var wg sync.WaitGroup
	for worker := range 8 {
		wg.Go(func() {
			for n := range 32 {
				i := (worker + n) % len(frames)
				got, err := decompress(frames[i], 0)
				if err != nil || !bytes.Equal(got, payloads[i]) {
					t.Errorf("worker %d, frame %d: decompress = %d bytes, %v; want %d matching bytes", worker, i, len(got), err, len(payloads[i]))
					return
				}
			}
		})
	}
	wg.Wait()
}

func TestDecompressPayloadOwnership(t *testing.T) {
	t.Parallel()
	want := bytes.Repeat([]byte("retained packet"), 1024)
	retained, err := decompress(compressedFrame(t, want), 0)
	if err != nil {
		t.Fatal(err)
	}
	for _, size := range []int{64, len(want), 65536, 2 * len(want)} {
		payload := bytes.Repeat([]byte{0xa5}, size)
		if _, err := decompress(compressedFrame(t, payload), 0); err != nil {
			t.Fatal(err)
		}
		if !bytes.Equal(retained, want) {
			t.Fatalf("retained payload changed after decoding %d bytes", size)
		}
	}
}

func TestDecompressValidAfterError(t *testing.T) {
	t.Parallel()
	payload := bytes.Repeat([]byte("valid packet"), 1024)
	valid := compressedFrame(t, payload)
	var invalidHeader, invalidBody, shortBody Buffer
	invalidHeader.PutVarInt(int32(len(payload)))
	invalidHeader.PutRaw([]byte{0, 0})
	invalidBody.PutVarInt(int32(len(payload)))
	invalidBody.PutRaw([]byte{0x78, 0x9c, 0xff})
	shortBody.PutVarInt(int32(len(payload) + 1))
	r := NewReader(valid)
	_ = r.ReadVarInt()
	shortBody.PutRaw(r.Rest())
	for _, bad := range [][]byte{invalidHeader.Bytes(), invalidBody.Bytes(), valid[:len(valid)/2], shortBody.Bytes()} {
		for range 3 {
			if _, err := decompress(bad, 0); err == nil {
				t.Fatal("invalid compressed frame accepted")
			}
			got, err := decompress(valid, 0)
			if err != nil || !bytes.Equal(got, payload) {
				t.Fatalf("valid frame after error: decompress = %d bytes, %v; want %d matching bytes", len(got), err, len(payload))
			}
		}
	}
}
