package server

import (
	"encoding/base64"
	"strings"

	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/internal/server/tickrate"
)

// Fixed world settings, each sent once at join and never again.

// sendTickingState sends the tick rate and frozen flag; the client ticks
// its view at this rate between corrections.
func (c *client) sendTickingState() error {
	return c.writePacket(protocol.IDPlayTickingState, "set ticking state",
		tickingStatePayload(c.srv.tickRate))
}

// tickingStatePayload builds Set Ticking State from the loop's state.
// vanilla: ClientboundTickingStatePacket
func tickingStatePayload(m *tickrate.Manager) []byte {
	var b protocol.Buffer
	b.PutF32(m.Rate())
	b.PutBool(m.Frozen())
	return b.Bytes()
}

// broadcastTickingState tells every player the new ticking state.
// vanilla: ServerTickRateManager.updateStateToClients
func (s *Server) broadcastTickingState() {
	s.broadcast(protocol.IDPlayTickingState, tickingStatePayload(s.tickRate))
}

// sendTickingStep sends Step Tick with the steps a frozen game has left.
func (c *client) sendTickingStep() error {
	return c.writePacket(protocol.IDPlayTickingStep, "step tick", tickingStepPayload(c.srv.tickRate))
}

// tickingStepPayload builds Step Tick from a frozen game's remaining steps.
func tickingStepPayload(m *tickrate.Manager) []byte {
	var b protocol.Buffer
	b.PutVarInt(int32(m.Steps()))
	return b.Bytes()
}

// broadcastTickingStep tells every player the remaining steps.
// vanilla: ServerTickRateManager.updateStepTicks
func (s *Server) broadcastTickingStep() {
	s.broadcast(protocol.IDPlayTickingStep, tickingStepPayload(s.tickRate))
}

// sendServerData sends the MOTD and icon the server list showed.
func (c *client) sendServerData() error {
	return c.writePacket(protocol.IDPlayServerData, "server data", c.serverDataPayload())
}

// serverDataPayload builds Server Data: the MOTD as a text component, and
// the icon's PNG bytes if there is one.
func (c *client) serverDataPayload() []byte {
	var b protocol.Buffer
	b.PutText(*c.srv.motd.Load())
	if icon, ok := faviconBytes(c.srv.favicon); ok {
		b.PutBool(true)
		b.PutVarInt(int32(len(icon)))
		b.PutRaw(icon)
		return b.Bytes()
	}
	b.PutBool(false) // no icon
	return b.Bytes()
}

// faviconBytes decodes the status data URI back to PNG bytes; ok is false
// with no icon or a bad one.
func faviconBytes(dataURI string) ([]byte, bool) {
	const prefix = "data:image/png;base64,"
	encoded, found := strings.CutPrefix(dataURI, prefix)
	if !found {
		return nil, false
	}
	icon, err := base64.StdEncoding.DecodeString(encoded)
	if err != nil {
		return nil, false
	}
	return icon, true
}
