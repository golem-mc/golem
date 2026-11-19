package protocol

import (
	"testing"
)

func TestJukeboxPlayableWire(t *testing.T) {
	t.Parallel()
	var registered, inline, bad Buffer
	registered.PutVarInt(RegistrySizeJukeboxSong)
	inline.PutVarInt(0)
	inline.PutVarInt(0) // inline SoundEvent
	inline.PutString("test:song")
	inline.PutBool(false)
	inline.PutText(Text{Text: "Song"})
	inline.PutF32(1)
	inline.PutVarInt(9)
	bad.PutVarInt(RegistrySizeJukeboxSong + 1)
	for _, tc := range []struct {
		name string
		data []byte
		ok   bool
	}{
		{"registered", registered.Bytes(), true},
		{"inline", inline.Bytes(), true},
		{"unknown", bad.Bytes(), false},
		{"truncated", inline.Bytes()[:len(inline.Bytes())-1], false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			stack := ItemStack{ItemID: 1, Count: 1, Raw: []RawComponent{{Type: DataComponentJukeboxPlayable, Data: tc.data}}}
			for _, untrusted := range []bool{false, true} {
				var wire Buffer
				if untrusted {
					wire.PutUntrustedSlot(stack)
				} else {
					wire.PutSlot(stack)
				}
				r := NewReader(wire.Bytes())
				read := r.ReadSlot
				if untrusted {
					read = r.ReadUntrustedSlot
				}
				got, ok := read()
				if ok != tc.ok || ok && (!got.Equal(stack) || r.Remaining() != 0) {
					t.Errorf("untrusted=%v: ok=%v, stack=%+v", untrusted, ok, got)
				}
			}
		})
	}
	stack := ItemStack{ItemID: 1, Count: 1}
	stack.SetJukeboxPlayable(0)
	copy := stack
	copy.SetJukeboxPlayable(1)
	if id, ok := stack.JukeboxPlayableOf(); !ok || id != 0 {
		t.Error("changing a copied stack changed the original song")
	}
	copy.Remove(DataComponentJukeboxPlayable)
	copy.SetJukeboxPlayable(0)
	if copy.Removes(DataComponentJukeboxPlayable) {
		t.Error("setting the song kept its removal")
	}
}
