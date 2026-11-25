package management

import (
	"os"
	"strings"
	"testing"

	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/internal/server/servertest"
	"github.com/golem-mc/golem/internal/server/world"
)

// vanilla: DedicatedServer.setViewDistance, PlayerList.setViewDistance,
// ChunkMap.setServerViewDistance, DedicatedServer.setSimulationDistance
func TestServerSettingsDistances(t *testing.T) {
	t.Parallel()
	srv, connect := servertest.Start(t, servertest.ManagementConfig(offline()), servertest.WriteAdminFiles(t, nil, nil, nil))
	c := servertest.ManagementClientFor(t, srv)
	// The fake client asks for a render distance of 4.
	alex := connect()
	alex.JoinAs("Alex")
	pos := alex.Pos()
	cx, cz := world.ChunkOf(pos.X), world.ChunkOf(pos.Z)
	forgotten := make(map[world.ChunkPos]bool)
	centre := world.ChunkPos{X: cx, Z: cz}
	for x := cx - alex.ChunkRadius - 1; x <= cx+alex.ChunkRadius+1; x++ {
		for z := cz - alex.ChunkRadius - 1; z <= cz+alex.ChunkRadius+1; z++ {
			p := world.ChunkPos{X: x, Z: z}
			if world.InSendView(p, centre, alex.ChunkRadius) && !world.InSendView(p, centre, 2) {
				forgotten[p] = false
			}
		}
	}
	if want := int(servertest.ViewChunks(alex.ChunkRadius) - servertest.ViewChunks(2)); len(forgotten) != want {
		t.Fatalf("join leaves %d chunks outside the view of 2, want %d", len(forgotten), want)
	}

	sameJSON(t, "view_distance", result(t, c, "serversettings/view_distance", nil), `10`)
	sameJSON(t, "view_distance/set 2", result(t, c, "serversettings/view_distance/set", []any{2}), `2`)
	// A connection ping can beat the independent chunk sender. Read both the
	// radius and the complete unload ring, without discarding either one's packets.
	radiusSeen, far := false, 0
	if !alex.ReadTicks(srv, 20, func(id int32, data []byte) bool {
		switch id {
		case protocol.IDPlaySetChunkCacheRadius:
			r := protocol.NewReader(data)
			if got := r.ReadVarInt(); got != 2 || r.Err() != nil {
				t.Fatalf("set_chunk_cache_radius = %d, error %v; want 2", got, r.Err())
			}
			radiusSeen = true
		case protocol.IDPlayUnloadChunk:
			r := protocol.NewReader(data)
			z, x := r.ReadI32(), r.ReadI32()
			if r.Err() != nil {
				t.Fatalf("unload chunk: %v", r.Err())
			}
			chunk := world.ChunkPos{X: x, Z: z}
			if seen, ok := forgotten[chunk]; !ok {
				t.Errorf("chunk (%d, %d) outside the removed ring was forgotten", x, z)
			} else if seen {
				t.Errorf("chunk (%d, %d) was forgotten twice", x, z)
			} else {
				forgotten[chunk] = true
				far++
			}
		}
		return radiusSeen && far == len(forgotten)
	}) {
		t.Fatalf("view shrink: radius packet seen = %t, forgot %d/%d chunks", radiusSeen, far, len(forgotten))
	}

	sameJSON(t, "view_distance/set 40", result(t, c, "serversettings/view_distance/set", []any{40}), `32`)
	if r := protocol.NewReader(alex.ExpectWithinTicks(srv, 20, protocol.IDPlaySetChunkCacheRadius)); r.ReadVarInt() != 32 || r.Err() != nil {
		t.Errorf("set_chunk_cache_radius does not carry 32: %v", r.Err())
	}

	sameJSON(t, "simulation_distance/set 3", result(t, c, "serversettings/simulation_distance/set", []any{3}), `3`)
	if r := protocol.NewReader(alex.ExpectWithinTicks(srv, 20, protocol.IDPlaySetSimulationDistance)); r.ReadVarInt() != 3 || r.Err() != nil {
		t.Errorf("set_simulation_distance does not carry 3: %v", r.Err())
	}

	props, err := os.ReadFile(srv.ConfigFile())
	if err != nil {
		t.Fatalf("read server.properties: %v", err)
	}
	for _, line := range []string{"view-distance=32", "simulation-distance=3"} {
		if !strings.Contains("\n"+string(props)+"\n", "\n"+line+"\n") {
			t.Errorf("server.properties lacks %q", line)
		}
	}
}
