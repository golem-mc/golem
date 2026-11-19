package server

import (
	"bytes"
	"testing"

	"github.com/golem-mc/golem/internal/protocol"
)

// TestParticlesPayloadWireOrder pins Level Particles' field order.
// vanilla: ClientboundLevelParticlesPacket
func TestParticlesPayloadWireOrder(t *testing.T) {
	var want protocol.Buffer
	want.PutVarInt(protocol.ParticleSplash)
	want.PutBool(true)
	want.PutBool(false)
	want.PutF64(1.5)
	want.PutF64(64)
	want.PutF64(-2)
	want.PutF32(0.25)
	want.PutF32(0.5)
	want.PutF32(0.75)
	for range 3 {
		want.PutF32(0.1)
	}
	want.PutVarInt(200)
	want.PutVarInt(0)

	got := particlesPayload(protocol.ParticleSplash, true, 1.5, 64, -2, 0.25, 0.5, 0.75, 0.1, 200)
	if !bytes.Equal(got, want.Bytes()) {
		t.Errorf("payload % X, want % X", got, want.Bytes())
	}
}
