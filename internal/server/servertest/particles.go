package servertest

import "github.com/golem-mc/golem/internal/protocol"

// LevelParticles is a decoded Level Particles packet of a type without
// options.
type LevelParticles struct {
	Type                   int32
	LongDistance, Always   bool
	X, Y, Z                float64
	DX, DY, DZ             float32
	SpeedX, SpeedY, SpeedZ float32
	Count                  int32
	// Randomization is 0 for DEFAULT, 1 for ALTERNATIVE.
	Randomization int32
}

// ParseLevelParticles decodes a Level Particles packet of a type without
// options; ok is false for a short or longer packet.
func ParseLevelParticles(data []byte) (p LevelParticles, ok bool) {
	r := protocol.NewReader(data)
	p.Type = r.ReadVarInt()
	p.LongDistance, p.Always = r.ReadBool(), r.ReadBool()
	p.X, p.Y, p.Z = r.ReadF64(), r.ReadF64(), r.ReadF64()
	p.DX, p.DY, p.DZ = r.ReadF32(), r.ReadF32(), r.ReadF32()
	p.SpeedX, p.SpeedY, p.SpeedZ = r.ReadF32(), r.ReadF32(), r.ReadF32()
	p.Count = r.ReadVarInt()
	p.Randomization = r.ReadVarInt()
	return p, r.Err() == nil && r.Remaining() == 0
}
