package main

import (
	"archive/zip"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/tools/internal/vanilla"
)

func configurationReference(t *testing.T) vanilla.Ref {
	t.Helper()
	ref := vanilla.Ref{Dir: t.TempDir(), Version: "26.3"}
	f, err := os.Create(filepath.Join(ref.Dir, "minecraft_client.26.3.jar"))
	if err != nil {
		t.Fatal(err)
	}
	z := zip.NewWriter(f)
	w, err := z.Create("version.json")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := w.Write([]byte(`{"id":"26.3","protocol_version":777}`)); err != nil {
		t.Fatal(err)
	}
	if err := z.Close(); err != nil {
		t.Fatal(err)
	}
	if err := f.Close(); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Dir(ref.ReportPath("packets.json")), 0o755); err != nil {
		t.Fatal(err)
	}
	// 26.3's configuration IDs.
	if err := os.WriteFile(ref.ReportPath("packets.json"), []byte(`{
		"handshake":{"serverbound":{"minecraft:intention":{"protocol_id":0}}},
		"login":{
			"serverbound":{"minecraft:hello":{"protocol_id":0},"minecraft:login_acknowledged":{"protocol_id":3}},
			"clientbound":{"minecraft:login_finished":{"protocol_id":2},"minecraft:login_compression":{"protocol_id":3},"minecraft:login_disconnect":{"protocol_id":0}}},
		"configuration":{
			"clientbound":{"minecraft:select_known_packs":{"protocol_id":15},"minecraft:registry_data":{"protocol_id":7},
				"minecraft:update_tags":{"protocol_id":14},"minecraft:server_links":{"protocol_id":17},
				"minecraft:finish_configuration":{"protocol_id":3},"minecraft:disconnect":{"protocol_id":2},
				"minecraft:keep_alive":{"protocol_id":4},"minecraft:post_effects":{"protocol_id":10}},
			"serverbound":{"minecraft:select_known_packs":{"protocol_id":7},"minecraft:client_information":{"protocol_id":0},
				"minecraft:finish_configuration":{"protocol_id":3},"minecraft:keep_alive":{"protocol_id":4}}}}`), 0o644); err != nil {
		t.Fatal(err)
	}
	return ref
}

func TestReferenceProfileUsesSelectedReleaseAndRejectsMissingPackets(t *testing.T) {
	ref := configurationReference(t)
	t.Setenv("REFERENCE_DIR", ref.Dir)
	t.Setenv("REFERENCE_VERSION", ref.Version)
	t.Setenv("CONFIG_ONLY", "1")
	p, err := profileFromEnv()
	if err != nil {
		t.Fatal(err)
	}
	if p.protocol != 777 || p.version != "26.3" || !p.configuration || p.incoming(15) != protocol.IDKnownPacks ||
		p.incoming(14) != protocol.IDConfigUpdateTags || p.incoming(17) != protocol.IDConfigServerLinks || p.incoming(10) != -1 {
		t.Errorf("profile = %+v", p)
	}

	// A play probe still uses the implemented protocol even with reference
	// variables set, since its play layouts have not been migrated yet.
	t.Setenv("CONFIG_ONLY", "")
	p, err = profileFromEnv()
	if err != nil || p.protocol != protocol.Version || p.configuration || p.incoming(15) != 15 {
		t.Errorf("native profile = %+v, %v", p, err)
	}

	if err := os.WriteFile(ref.ReportPath("packets.json"), []byte(`{}`), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := referenceProfile(ref); err == nil || !strings.Contains(err.Error(), "minecraft:intention") {
		t.Errorf("missing report entry: %v", err)
	}
}

func TestReferenceConfigurationCapturesShiftedIDsAndAnswersKeepAlive(t *testing.T) {
	p, err := referenceProfile(configurationReference(t))
	if err != nil {
		t.Fatal(err)
	}
	before := dump
	dump.Registries, dump.Tags = nil, nil
	t.Cleanup(func() { dump = before })
	client, server := configurationPipe(t)
	finished := make(chan error, 1)
	go func() { finished <- handleConfiguration(client, p) }()

	var packs protocol.Buffer
	packs.PutVarInt(1)
	packs.PutString("minecraft")
	packs.PutString("core")
	packs.PutString("26.3")
	writeConfigurationPacket(t, server, 15, packs.Bytes())
	id, data, err := server.ReadPacket()
	if err != nil || id != 7 || !slices.Equal(data, packs.Bytes()) {
		t.Fatalf("known packs response = %d %x, %v", id, data, err)
	}
	if id, _, err := server.ReadPacket(); err != nil || id != 0 {
		t.Fatalf("client information = %d, %v", id, err)
	}
	var keepalive protocol.Buffer
	keepalive.PutI64(123456)
	writeConfigurationPacket(t, server, 4, keepalive.Bytes())
	if id, data, err := server.ReadPacket(); err != nil || id != 4 || !slices.Equal(data, keepalive.Bytes()) {
		t.Fatalf("keep-alive response = %d %x, %v", id, data, err)
	}
	var registry protocol.Buffer
	registry.PutString("minecraft:worldgen/biome")
	registry.PutVarInt(1)
	registry.PutString("minecraft:plains")
	registry.PutBool(false)
	writeConfigurationPacket(t, server, 7, registry.Bytes())
	var tags protocol.Buffer
	tags.PutVarInt(1)
	tags.PutString("minecraft:worldgen/biome")
	tags.PutVarInt(1)
	tags.PutString("minecraft:is_overworld")
	tags.PutVarInt(1)
	tags.PutVarInt(0)
	writeConfigurationPacket(t, server, 14, tags.Bytes())
	writeConfigurationPacket(t, server, 3, nil)
	if id, _, err := server.ReadPacket(); err != nil || id != 3 {
		t.Fatalf("finish response = %d, %v", id, err)
	}
	if err := <-finished; err != nil {
		t.Fatal(err)
	}
	if len(dump.Registries) != 1 || !slices.Equal(dump.Registries[0].Entries, []string{"minecraft:plains"}) ||
		len(dump.Tags) != 1 || len(dump.Tags[0].Tags) != 1 || !slices.Equal(dump.Tags[0].Tags[0].IDs, []int32{0}) {
		t.Errorf("dump = %+v", dump)
	}
}

func TestReferenceLoginSendsSelectedProtocolAndRejectsChangedLayout(t *testing.T) {
	p, err := referenceProfile(configurationReference(t))
	if err != nil {
		t.Fatal(err)
	}
	client, server := configurationPipe(t)
	finished := make(chan error, 1)
	go func() {
		finished <- handleLogin(client, protocol.Handshake{
			ProtocolVersion: p.protocol, ServerAddress: "localhost", ServerPort: 25565, NextState: protocol.NextStateLogin,
		})
	}()
	id, data, err := server.ReadPacket()
	if err != nil || id != protocol.IDHandshake {
		t.Fatalf("handshake = %d, %v", id, err)
	}
	h, err := protocol.ReadHandshake(data)
	if err != nil || h.ProtocolVersion != 777 {
		t.Fatalf("handshake = %+v, %v", h, err)
	}
	if id, _, err := server.ReadPacket(); err != nil || id != protocol.IDLoginStart {
		t.Fatalf("login start = %d, %v", id, err)
	}
	writeConfigurationPacket(t, server, protocol.IDLoginSuccess, nil)
	if err := <-finished; err == nil || !strings.Contains(err.Error(), "login success") {
		t.Errorf("changed login layout = %v", err)
	}
}

func configurationPipe(t *testing.T) (*protocol.Conn, *protocol.Conn) {
	t.Helper()
	a, b := net.Pipe()
	if err := b.SetDeadline(time.Now().Add(5 * time.Second)); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { a.Close(); b.Close() })
	return protocol.NewConn(a), protocol.NewConn(b)
}

func writeConfigurationPacket(t *testing.T, c *protocol.Conn, id int32, data []byte) {
	t.Helper()
	if err := c.WritePacket(id, data); err != nil {
		t.Fatal(fmt.Errorf("write configuration 0x%02x: %w", id, err))
	}
}
