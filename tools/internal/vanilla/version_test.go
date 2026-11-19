package vanilla

import (
	"archive/zip"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestVersionInfoReadsJarMetadataAndRejectsAnotherRelease(t *testing.T) {
	ref := Ref{Dir: t.TempDir(), Version: "26.3"}
	f, err := os.Create(filepath.Join(ref.Dir, "minecraft_client.26.3.jar"))
	if err != nil {
		t.Fatal(err)
	}
	z := zip.NewWriter(f)
	w, err := z.Create("version.json")
	if err != nil {
		t.Fatal(err)
	}
	_, err = w.Write([]byte(`{"id":"26.3","name":"26.3","world_version":5023,"series_id":"main","protocol_version":777,
		"pack_version":{"resource_major":97,"resource_minor":1,"data_major":121,"data_minor":0},
		"build_time":"2026-09-15T11:20:48+00:00","java_version":25,"stable":true}`))
	if err != nil {
		t.Fatal(err)
	}
	if err := z.Close(); err != nil {
		t.Fatal(err)
	}
	if err := f.Close(); err != nil {
		t.Fatal(err)
	}
	v, err := ref.VersionInfo()
	if err != nil {
		t.Fatal(err)
	}
	if v.ID != "26.3" || v.Name != v.ID || v.ProtocolVersion != 777 || v.WorldVersion != 5023 ||
		v.SeriesID != "main" || v.JavaVersion != 25 || !v.Stable || v.BuildTime.Unix() != 1789471248 ||
		v.PackVersion.ResourceMajor != 97 || v.PackVersion.ResourceMinor != 1 ||
		v.PackVersion.DataMajor != 121 || v.PackVersion.DataMinor != 0 {
		t.Errorf("version.json = %+v", v)
	}

	// ClientJar prefers a fully installed client; a leftover one must not
	// silently supply the metadata of the release being migrated away from.
	if err := os.Mkdir(filepath.Join(ref.Dir, "client"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Rename(f.Name(), filepath.Join(ref.Dir, "client", "client.jar")); err != nil {
		t.Fatal(err)
	}
	ref.Version = "26.1"
	if _, err := ref.VersionInfo(); err == nil || !strings.Contains(err.Error(), `wanted 26.1, got "26.3"`) {
		t.Errorf("mismatched jar: %v", err)
	}
}

func TestPacketReportResolvesIDsAndRejectsMissingEntries(t *testing.T) {
	ref := writeReports(t, "packets.json", `{"configuration":{"clientbound":{
		"minecraft:select_known_packs":{"protocol_id":15},"minecraft:cookie_request":{"protocol_id":0},
		"minecraft:invalid":{"protocol_id":-1}}}}`)
	packets, err := ref.Packets()
	if err != nil {
		t.Fatal(err)
	}
	for name, want := range map[string]int32{"select_known_packs": 15, "cookie_request": 0} {
		if id, err := packets.ID("configuration", "clientbound", "minecraft:"+name); err != nil || id != want {
			t.Errorf("%s: %d, %v; want %d", name, id, err, want)
		}
	}
	for _, args := range [][3]string{
		{"play", "clientbound", "minecraft:cookie_request"},
		{"configuration", "serverbound", "minecraft:cookie_request"},
		{"configuration", "clientbound", "minecraft:missing"},
		{"configuration", "clientbound", "minecraft:invalid"},
	} {
		if _, err := packets.ID(args[0], args[1], args[2]); err == nil {
			t.Errorf("%v: missing packet accepted", args)
		}
	}
}
