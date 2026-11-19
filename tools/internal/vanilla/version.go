package vanilla

import (
	"archive/zip"
	"encoding/json"
	"fmt"
	"time"
)

// VersionInfo is version.json from the reference jar, not launcher metadata.
type VersionInfo struct {
	ID              string    `json:"id"`
	Name            string    `json:"name"`
	WorldVersion    int       `json:"world_version"`
	SeriesID        string    `json:"series_id"`
	ProtocolVersion int32     `json:"protocol_version"`
	BuildTime       time.Time `json:"build_time"`
	JavaVersion     int       `json:"java_version"`
	Stable          bool      `json:"stable"`
	PackVersion     struct {
		ResourceMajor int `json:"resource_major"`
		ResourceMinor int `json:"resource_minor"`
		DataMajor     int `json:"data_major"`
		DataMinor     int `json:"data_minor"`
	} `json:"pack_version"`
}

// VersionInfo reads the selected client jar and refuses a different release.
func (r Ref) VersionInfo() (VersionInfo, error) {
	var v VersionInfo
	jar, err := r.ClientJar()
	if err != nil {
		return v, err
	}
	zr, err := zip.OpenReader(jar)
	if err != nil {
		return v, err
	}
	defer zr.Close()
	f, err := zr.Open("version.json")
	if err != nil {
		return v, fmt.Errorf("%s: %w", jar, err)
	}
	defer f.Close()
	if err := json.NewDecoder(f).Decode(&v); err != nil {
		return v, fmt.Errorf("%s version.json: %w", jar, err)
	}
	if v.ID != r.Version || v.ProtocolVersion <= 0 {
		return v, fmt.Errorf("%s version.json: wanted %s, got %q (protocol %d)", jar, r.Version, v.ID, v.ProtocolVersion)
	}
	return v, nil
}

// Packets is packets.json, keyed by state, direction and namespaced packet name.
type Packets map[string]map[string]map[string]Entry

// Packets reads the packet IDs of the reference server.
func (r Ref) Packets() (Packets, error) {
	var packets Packets
	if err := r.readJSON(r.ReportPath("packets.json"), &packets); err != nil {
		return nil, err
	}
	return packets, nil
}

// ID resolves a packet without treating a missing entry as packet zero.
func (p Packets) ID(state, direction, name string) (int32, error) {
	e, ok := p[state][direction][name]
	if !ok || e.ProtocolID < 0 {
		return 0, fmt.Errorf("packets.json has no valid %s %s %s", state, direction, name)
	}
	return int32(e.ProtocolID), nil
}
