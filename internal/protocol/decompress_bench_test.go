package protocol

import (
	"bytes"
	"strconv"
	"testing"

	"github.com/klauspost/compress/zlib"
)

func BenchmarkDecompressPacket(b *testing.B) {
	for _, size := range []int{64, 512, 4096, 65536} {
		b.Run(strconv.Itoa(size), func(b *testing.B) {
			payload := make([]byte, size)
			for i := range payload {
				payload[i] = byte(i*7 + i/128)
			}
			var compressed bytes.Buffer
			zw := zlib.NewWriter(&compressed)
			if _, err := zw.Write(payload); err != nil {
				b.Fatal(err)
			}
			if err := zw.Close(); err != nil {
				b.Fatal(err)
			}
			var frame Buffer
			frame.PutVarInt(int32(size))
			frame.PutRaw(compressed.Bytes())
			b.SetBytes(int64(size))
			b.ReportAllocs()
			for b.Loop() {
				out, err := decompress(frame.Bytes(), 0)
				if err != nil || len(out) != size {
					b.Fatalf("decompress = %d bytes, %v; want %d bytes", len(out), err, size)
				}
			}
		})
	}
}
