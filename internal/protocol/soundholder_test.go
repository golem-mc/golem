package protocol

import (
	"bytes"
	"testing"
)

func TestPutSoundHolderEncoding(t *testing.T) {
	for _, tc := range []struct {
		name  string
		sound SoundHolder
		want  []byte
	}{
		{"first registered", SoundHolder{ID: 0}, []byte{1}},
		{"multibyte registered", SoundHolder{ID: 127}, []byte{0x80, 1}},
		{"inline", SoundHolder{ID: 127, Name: "test:sound"}, []byte("\x00\x0atest:sound\x00")},
		{"inline range", SoundHolder{Name: "test:sound", HasRange: true, Range: 32}, []byte("\x00\x0atest:sound\x01\x42\x00\x00\x00")},
		{"empty fallback", SoundHolder{ID: -1}, []byte{0, 0, 0}},
		{"empty fallback range", SoundHolder{ID: -1, HasRange: true, Range: 32}, []byte{0, 0, 1, 0x42, 0, 0, 0}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var b Buffer
			PutSoundHolder(&b, tc.sound)
			if !bytes.Equal(b.Bytes(), tc.want) {
				t.Fatalf("holder = %x, want %x", b.Bytes(), tc.want)
			}
		})
	}
}
