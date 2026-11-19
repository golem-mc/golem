package redstone

import (
	"testing"

	"github.com/golem-mc/golem/internal/config"
	"github.com/golem-mc/golem/internal/server/servertest"
	"github.com/golem-mc/golem/registry"
)

func TestTrumpetNoteSoundIDs(t *testing.T) {
	t.Parallel()
	cfg := servertest.BlockTestConfig()
	cfg.ViewDistance = config.MinDistance
	cfg.SimulationDistance = config.MinDistance
	srv, c := servertest.Join(t, cfg, "Trumpeter")
	servertest.HoldTicks(t, srv)
	for _, instrument := range []string{"trumpet", "trumpet_exposed", "trumpet_oxidized", "trumpet_weathered"} {
		state, ok := registry.WithProp(registry.BlockNoteBlock.DefaultState(), registry.PropInstrument, instrument)
		if !ok || !srv.SetBlock(4, 64, 2, state) {
			t.Fatal(instrument)
		}
		var heard noteHeard
		c.UseBlock(4, 64, 2)
		c.SettleAndCollect(func(id int32, data []byte) { heard.recordNote(t, 4, 64, 2, id, data) })
		heard.wantTones(t, instrument, 1)
	}
}
