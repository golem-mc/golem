package network

import (
	"encoding/json"
	"github.com/golem-mc/golem/internal/server/servertest"
	"testing"

	"github.com/golem-mc/golem/internal/config"
	"github.com/golem-mc/golem/internal/protocol"
)

// statusSample pings the server on a fresh connection and returns the
// players it lists.
func statusSample(t *testing.T, connect func() *servertest.Client) ([]protocol.StatusPlayer, int) {
	t.Helper()
	c := connect()
	c.Handshake(protocol.NextStateStatus)
	c.Send(protocol.IDStatusRequest, nil)
	r := protocol.NewReader(c.Expect(protocol.IDStatusResponse))
	var resp protocol.StatusResponse
	if err := json.Unmarshal([]byte(r.ReadString(32767)), &resp); err != nil {
		t.Fatalf("unmarshal status: %v", err)
	}
	return resp.Players.Sample, resp.Players.Online
}

func joinForStatus(t *testing.T, connect func() *servertest.Client, name string, unlisted bool) {
	t.Helper()
	c := connect()
	c.Unlisted = unlisted
	c.Join(name)
}

func TestStatusSampleListsOnlinePlayers(t *testing.T) {
	t.Parallel()
	cfg := config.Default()
	cfg.OnlineMode = false
	_, connect := servertest.Start(t, cfg)
	joinForStatus(t, connect, "Alice", false)
	joinForStatus(t, connect, "Bob", false)
	joinForStatus(t, connect, "Carol", true)

	sample, online := statusSample(t, connect)
	if online != 3 {
		t.Errorf("online = %d, want 3", online)
	}
	got := map[string]string{}
	for _, p := range sample {
		got[p.Name] = p.ID
	}
	for _, name := range []string{"Alice", "Bob"} {
		if want := protocol.OfflineUUID(name).String(); got[name] != want {
			t.Errorf("sample[%s] = %q, want %q", name, got[name], want)
		}
	}
	if _, ok := got["Carol"]; ok {
		t.Error("a player who disallows listing is named in the sample")
	}
	if len(sample) != 3 || got["Anonymous Player"] != "00000000-0000-0000-0000-000000000000" {
		t.Errorf("sample = %v, want Alice, Bob and one Anonymous Player", sample)
	}
}

func TestStatusSampleHiddenByConfig(t *testing.T) {
	t.Parallel()
	cfg := config.Default()
	cfg.OnlineMode = false
	cfg.HideOnlinePlayers = true
	_, connect := servertest.Start(t, cfg)
	joinForStatus(t, connect, "Alice", false)

	sample, online := statusSample(t, connect)
	if len(sample) != 0 {
		t.Errorf("sample = %v, want none with hide-online-players", sample)
	}
	if online != 1 {
		t.Errorf("online = %d, want 1", online)
	}
}
