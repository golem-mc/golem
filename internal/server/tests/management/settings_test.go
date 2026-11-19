package management

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/internal/server/servertest"
	"github.com/golem-mc/golem/internal/server/world"
	"github.com/golem-mc/golem/registry"
)

// vanilla: ServerSettingsService, MinecraftServerSettingsServiceImpl,
// DedicatedServer's setters
func TestServerSettingsMethods(t *testing.T) {
	t.Parallel()
	cfg := servertest.ManagementConfig(offline())
	srv, connect := servertest.Start(t, cfg, servertest.WriteAdminFiles(t, nil, nil, nil), servertest.WriteOpsFile(t))
	c := servertest.ManagementClientFor(t, srv)

	for _, tc := range []struct {
		name, key string
		old, set  any
	}{
		{"autosave", "", true, false},
		{"max_players", "max-players", cfg.MaxPlayers, 1},
		{"motd", "motd", cfg.MOTD, "Hello"},
		{"spawn_protection_radius", "spawn-protection", cfg.SpawnProtection, 23},
		{"pause_when_empty_seconds", "pause-when-empty-seconds", cfg.PauseWhenEmptySeconds, 120},
		{"accept_transfers", "accepts-transfers", cfg.AcceptsTransfers, !cfg.AcceptsTransfers},
		{"hide_online_players", "hide-online-players", cfg.HideOnlinePlayers, !cfg.HideOnlinePlayers},
		{"status_replies", "enable-status", cfg.EnableStatus, !cfg.EnableStatus},
		{"entity_broadcast_range", "entity-broadcast-range-percentage", cfg.EntityBroadcastRangePercentage, 75},
		{"use_allowlist", "white-list", cfg.WhiteList, true},
		{"enforce_allowlist", "enforce-whitelist", cfg.EnforceWhitelist, true},
		{"player_idle_timeout", "player-idle-timeout", cfg.PlayerIdleTimeout, 30},
		{"allow_flight", "allow-flight", cfg.AllowFlight, !cfg.AllowFlight},
		{"status_heartbeat_interval", "status-heartbeat-interval", cfg.StatusHeartbeatInterval, 3600},
		{"operator_user_permission_level", "op-permission-level", cfg.OpPermissionLevel, 2},
	} {
		method := "serversettings/" + tc.name
		want := func(v any) string {
			encoded, err := json.Marshal(v)
			if err != nil {
				t.Fatal(err)
			}
			return string(encoded)
		}
		sameJSON(t, method, result(t, c, method, nil), want(tc.old))
		sameJSON(t, method+"/set", result(t, c, method+"/set", []any{tc.set}), want(tc.set))
		sameJSON(t, method+" after set", result(t, c, method, nil), want(tc.set))
		if tc.key == "" {
			continue
		}
		props, err := os.ReadFile(srv.ConfigFile())
		if err != nil {
			t.Fatalf("read server.properties: %v", err)
		}
		if line := tc.key + "=" + fmt.Sprint(tc.set); !strings.Contains("\n"+string(props)+"\n", "\n"+line+"\n") {
			t.Errorf("server.properties after %s/set lacks %q", tc.name, line)
		}
	}

	// vanilla: PermissionLevel.INT_CODEC clamps through ByIdMap.
	sameJSON(t, "clamped level", result(t, c, "serversettings/operator_user_permission_level/set", []any{7}), `4`)
	result(t, c, "serversettings/operator_user_permission_level/set", []any{2})
	result(t, c, "serversettings/use_allowlist/set", []any{false})
	dave := connect()
	dave.JoinAs("Dave")
	sameJSON(t, "default op level", result(t, c, "operators/add",
		[]any{[]any{map[string]any{"player": map[string]any{"name": "Dave"}}}}), `[`+opJSON("Dave", 2, false)+`]`)

	// autosave/set false is /save-off's flag, so /save-on then reports a change.
	if got := srv.RunLine("save-on"); len(got) != 1 || got[0].Translate != "commands.save.enabled" {
		t.Errorf("save-on after autosave/set false = %+v, want commands.save.enabled", got)
	}
}

// vanilla: DedicatedServer.setUsingWhitelist, setEnforceWhitelist,
// MinecraftServer.kickUnlistedPlayers
func TestServerSettingsAllowlistKicks(t *testing.T) {
	t.Parallel()
	srv, connect := servertest.Start(t, servertest.ManagementConfig(offline()), servertest.WriteAdminFiles(t, nil, nil, nil))
	c := servertest.ManagementClientFor(t, srv)
	bob := connect()
	bob.JoinAs("Bob")

	sameJSON(t, "enforce", result(t, c, "serversettings/enforce_allowlist/set", []any{true}), `true`)
	sameJSON(t, "use", result(t, c, "serversettings/use_allowlist/set", []any{true}), `true`)
	servertest.ExpectDisconnect(t, bob, protocol.Text{Translate: "multiplayer.disconnect.not_whitelisted"})
}

// vanilla: DedicatedServer's mutable properties, PlayerList.canPlayerLogin,
// MinecraftServer.buildServerStatus
func TestServerSettingsLiveLoginAndStatus(t *testing.T) {
	t.Parallel()
	srv, connect := servertest.Start(t, servertest.ManagementConfig(offline()), servertest.WriteAdminFiles(t, nil, nil, nil))
	c := servertest.ManagementClientFor(t, srv)
	alice := connect()
	alice.JoinAs("Alice")
	status := func() protocol.StatusResponse {
		t.Helper()
		ping := connect()
		defer ping.Raw.Close()
		ping.Handshake(protocol.NextStateStatus)
		ping.Send(protocol.IDStatusRequest, nil)
		r := protocol.NewReader(ping.Expect(protocol.IDStatusResponse))
		var resp protocol.StatusResponse
		if err := json.Unmarshal([]byte(r.ReadString(32767)), &resp); err != nil {
			t.Fatal(err)
		}
		return resp
	}
	before := status()
	if len(before.Players.Sample) != 1 || before.Players.Sample[0].Name != "Alice" {
		t.Fatalf("initial status sample = %v, want Alice", before.Players.Sample)
	}
	for _, value := range []struct{ set, want int }{{5, 10}, {1001, 1000}} {
		sameJSON(t, "clamped range", result(t, c, "serversettings/entity_broadcast_range/set", []any{value.set}), fmt.Sprint(value.want))
		sameJSON(t, "range getter", result(t, c, "serversettings/entity_broadcast_range", nil), fmt.Sprint(value.want))
		props, err := os.ReadFile(srv.ConfigFile())
		if err != nil {
			t.Fatal(err)
		}
		if !strings.Contains(string(props), fmt.Sprintf("entity-broadcast-range-percentage=%d\n", value.want)) {
			t.Fatalf("clamped range %d not persisted", value.want)
		}
	}
	sameJSON(t, "player limit", result(t, c, "serversettings/max_players/set", []any{1}), `1`)
	bob := connect()
	bob.SendLoginStart("Bob")
	if got := protocol.NewReader(bob.Expect(protocol.IDLoginDisconnect)).ReadString(32767); got != `{"translate":"multiplayer.disconnect.server_full"}` {
		t.Fatalf("second login = %s, want server_full", got)
	}
	sameJSON(t, "motd", result(t, c, "serversettings/motd/set", []any{"Hello"}), `"Hello"`)
	after := status()
	if after.Description.Text != "Hello" || after.Players.Max != 1 || after.Players.Online != 1 {
		t.Errorf("status after set = %+v, want Hello and 1/1 players", after)
	}
	sameJSON(t, "hide", result(t, c, "serversettings/hide_online_players/set", []any{true}), `true`)
	after = status()
	if len(after.Players.Sample) != 0 || after.Players.Online != 1 {
		t.Errorf("hidden status = %+v, want empty sample and 1 online", after.Players)
	}
}

// vanilla: ServerHandshakePacketListenerImpl.handleIntention
func TestServerSettingsLiveConnectionGates(t *testing.T) {
	t.Parallel()
	srv, connect := servertest.Start(t, servertest.ManagementConfig(offline()), servertest.WriteAdminFiles(t, nil, nil, nil))
	c := servertest.ManagementClientFor(t, srv)
	for _, enabled := range []bool{true, false} {
		result(t, c, "serversettings/accept_transfers/set", []any{enabled})
		transfer := connect()
		if enabled {
			transfer.LoginWithIntent("Visitor", protocol.NextStateTransfer)
		} else {
			transfer.Handshake(protocol.NextStateTransfer)
			if got := protocol.NewReader(transfer.Expect(protocol.IDLoginDisconnect)).ReadString(32767); got != `{"translate":"multiplayer.disconnect.transfers_disabled"}` {
				t.Fatalf("transfer refusal = %s, want transfers_disabled", got)
			}
		}
		transfer.Raw.Close()
	}
	result(t, c, "serversettings/status_replies/set", []any{false})
	ping := connect()
	ping.Handshake(protocol.NextStateStatus)
	if err := ping.Raw.SetReadDeadline(time.Now().Add(servertest.ExpectTimeout)); err != nil {
		t.Fatal(err)
	}
	if _, _, err := ping.Conn.ReadPacket(); !errors.Is(err, io.EOF) && !errors.Is(err, io.ErrUnexpectedEOF) {
		t.Fatalf("disabled status read = %v, want connection closed", err)
	}
	result(t, c, "serversettings/status_replies/set", []any{true})
	ping = connect()
	ping.Handshake(protocol.NextStateStatus)
	ping.Send(protocol.IDStatusRequest, nil)
	ping.Expect(protocol.IDStatusResponse)
}

// vanilla: DedicatedServer.getScaledTrackingDistance
func TestServerSettingsLiveTrackingRange(t *testing.T) {
	t.Parallel()
	srv, connect := servertest.Start(t, servertest.ManagementConfig(servertest.MobTestConfig()), servertest.WriteAdminFiles(t, nil, nil, nil))
	c := servertest.ManagementClientFor(t, srv)
	watcher := connect().Join("Watcher")
	servertest.HoldTicks(t, srv)
	cow := srv.SpawnMob("minecraft:cow", 30.5, 64, 0.5)
	watcher.ExpectMobSpawn(protocol.EntityTypeCow)
	result(t, c, "serversettings/entity_broadcast_range/set", []any{5})
	servertest.AwaitRemovalsOfTicks(t, watcher, srv, 10, cow)
	result(t, c, "serversettings/entity_broadcast_range/set", []any{100})
	spawn := protocol.NewReader(watcher.ExpectWithinTicks(srv, 10, protocol.IDPlaySpawnEntity))
	if id := spawn.ReadVarInt(); id != cow {
		t.Fatalf("restored range spawned entity %d, want cow %d", id, cow)
	}
}

// vanilla: DedicatedServer.isUnderSpawnProtection
func TestServerSettingsLiveSpawnProtection(t *testing.T) {
	t.Parallel()
	srv, connect := servertest.Start(t, servertest.ManagementConfig(servertest.BlockTestConfig()), servertest.WriteAdminFiles(t, nil, nil, nil), servertest.OpsListing(t, "Admin"))
	c := servertest.ManagementClientFor(t, srv)
	digger := connect().Join("Digger")
	servertest.HoldTicks(t, srv)
	srv.SetWorldSpawn(world.SpawnPos{X: 0, Y: 64, Z: 0})
	stone := registry.BlockStone.DefaultState()
	if !srv.SetBlock(1, 64, 0, stone) {
		t.Fatal("could not place target stone")
	}
	for _, radius := range []int{16, 0} {
		result(t, c, "serversettings/spawn_protection_radius/set", []any{radius})
		digger.StartDigging(1, 64, 0)
		digger.ExpectWithinTicks(srv, 3, protocol.IDPlayAckBlockChange)
		want := stone
		if radius == 0 {
			want = 0
		}
		if got := srv.BlockAt(1, 64, 0); got != want {
			t.Fatalf("block with spawn protection %d = %d, want %d", radius, got, want)
		}
	}
}
