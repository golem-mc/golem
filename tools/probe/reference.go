package main

import (
	"fmt"
	"os"

	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/tools/internal/vanilla"
)

// configurationProfile lets the registry capture precede a protocol migration.
// It changes IDs only: a changed layout must still fail the probe's readers.
type configurationProfile struct {
	version       string
	protocol      int32
	configuration bool
	in, out       map[int32]int32
}

func profileFromEnv() (configurationProfile, error) {
	p := configurationProfile{version: protocol.GameVersion, protocol: protocol.Version}
	if os.Getenv("CONFIG_ONLY") == "" {
		return p, nil
	}
	dir := os.Getenv("REFERENCE_DIR")
	if dir == "" {
		dir = "reference-server"
	}
	return referenceProfile(vanilla.Ref{Dir: dir, Version: vanilla.VersionFromEnv()})
}

func referenceProfile(ref vanilla.Ref) (configurationProfile, error) {
	p := configurationProfile{configuration: true, in: map[int32]int32{}, out: map[int32]int32{}}
	v, err := ref.VersionInfo()
	if err != nil {
		return p, err
	}
	p.version, p.protocol = v.ID, v.ProtocolVersion
	packets, err := ref.Packets()
	if err != nil {
		return p, err
	}
	for _, entry := range []struct {
		state, direction, name string
		id                     int32
	}{
		{"handshake", "serverbound", "intention", protocol.IDHandshake},
		{"login", "serverbound", "hello", protocol.IDLoginStart},
		{"login", "serverbound", "login_acknowledged", protocol.IDLoginAcknowledged},
		{"login", "clientbound", "login_finished", protocol.IDLoginSuccess},
		{"login", "clientbound", "login_compression", protocol.IDSetCompression},
		{"login", "clientbound", "login_disconnect", protocol.IDLoginDisconnect},
		{"configuration", "clientbound", "select_known_packs", protocol.IDKnownPacks},
		{"configuration", "clientbound", "registry_data", protocol.IDRegistryData},
		{"configuration", "clientbound", "update_tags", protocol.IDConfigUpdateTags},
		{"configuration", "clientbound", "server_links", protocol.IDConfigServerLinks},
		{"configuration", "clientbound", "finish_configuration", protocol.IDConfigFinish},
		{"configuration", "clientbound", "disconnect", protocol.IDConfigDisconnect},
		{"configuration", "clientbound", "keep_alive", protocol.IDConfigKeepAlive},
		{"configuration", "serverbound", "select_known_packs", protocol.IDKnownPacksIn},
		{"configuration", "serverbound", "client_information", protocol.IDClientInformation},
		{"configuration", "serverbound", "finish_configuration", protocol.IDConfigFinishAck},
		{"configuration", "serverbound", "keep_alive", protocol.IDConfigKeepAliveIn},
	} {
		id, err := packets.ID(entry.state, entry.direction, "minecraft:"+entry.name)
		if err != nil {
			return p, err
		}
		if entry.state != "configuration" {
			if id != entry.id {
				return p, fmt.Errorf("%s %s %s changed ID: update the login probe", entry.state, entry.direction, entry.name)
			}
		} else if entry.direction == "clientbound" {
			p.in[id] = entry.id
		} else {
			p.out[entry.id] = id
		}
	}
	return p, nil
}

func (p configurationProfile) incoming(id int32) int32 {
	if p.in == nil {
		return id
	}
	if canonical, ok := p.in[id]; ok {
		return canonical
	}
	// A newly added packet can reuse an old ID that had a different layout.
	return -1
}

func (p configurationProfile) send(c *protocol.Conn, id int32, data []byte) error {
	if p.out != nil {
		mapped, ok := p.out[id]
		if !ok {
			return fmt.Errorf("no reference configuration packet for 0x%02X", id)
		}
		id = mapped
	}
	return c.WritePacket(id, data)
}
