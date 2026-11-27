package storage

import (
	"bytes"
	"compress/gzip"
	"os"
	"path/filepath"
	"testing"

	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/internal/server/servertest"
	"github.com/golem-mc/golem/nbt"
)

// TestScoreboardFromBeforeTheTextComponentChangeKeepsItsDisplays writes a 1.19.4
// scoreboard.dat: its slot_1 sidebar must still display, and the JSON strings
// of its objective and team must reach the client as components.
// vanilla: ScoreboardDisplaySlotFix, UnflattenTextComponentFix
func TestScoreboardFromBeforeTheTextComponentChangeKeepsItsDisplays(t *testing.T) {
	t.Parallel()
	cfg := servertest.BlockTestConfig()
	level := t.TempDir()
	cfg.LevelName = level
	data := nbt.Compound{
		"Objectives": nbt.List{Elem: nbt.TypeCompound, Items: []nbt.Tag{nbt.Compound{
			"Name": nbt.String("kills"), "CriteriaName": nbt.String("dummy"), "RenderType": nbt.String("integer"),
			"DisplayName": nbt.String(`{"text":"Kills","color":"red"}`),
		}}},
		"PlayerScores": nbt.List{Elem: nbt.TypeEnd},
		"DisplaySlots": nbt.Compound{"slot_1": nbt.String("kills")},
		"Teams": nbt.List{Elem: nbt.TypeCompound, Items: []nbt.Tag{nbt.Compound{
			"Name": nbt.String("a"), "DisplayName": nbt.String(`{"text":"A"}`),
			"MemberNamePrefix": nbt.String(`{"text":"[A] "}`), "MemberNameSuffix": nbt.String(`{"text":""}`),
			"TeamColor": nbt.String("none"), "NameTagVisibility": nbt.String("always"),
			"CollisionRule": nbt.String("always"), "Players": nbt.List{Elem: nbt.TypeString, Items: []nbt.Tag{nbt.String("Alice")}},
		}}},
	}
	raw, err := nbt.Encode("", nbt.Compound{"data": data, "DataVersion": nbt.Int(3337)})
	if err != nil {
		t.Fatal(err)
	}
	var packed bytes.Buffer
	gz := gzip.NewWriter(&packed)
	gz.Write(raw)
	gz.Close()
	path := filepath.Join(level, "data", "minecraft", "scoreboard.dat")
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, packed.Bytes(), 0o644); err != nil {
		t.Fatal(err)
	}

	_, reconnect := servertest.Start(t, cfg)
	c := reconnect()
	c.Login("Joiner")
	c.Configure()
	var title protocol.Text
	var sidebar, prefix string
	var prefixColor string
	c.ReadJoin(func(id int32, payload []byte) {
		r := protocol.NewReader(payload)
		switch id {
		case protocol.IDPlayUpdateObjectives:
			r.ReadString(32767)
			r.ReadU8()
			title, _ = protocol.ReadText(r)
		case protocol.IDPlayDisplayObjective:
			if r.ReadVarInt() == 1 {
				sidebar = r.ReadString(32767)
			}
		case protocol.IDPlaySetPlayerTeam:
			r.ReadString(32767)
			if r.ReadU8() == 0 {
				protocol.ReadText(r)
				p, _ := protocol.ReadText(r)
				prefix, prefixColor = p.Text, p.Color
			}
		}
	})
	if sidebar != "kills" {
		t.Errorf("sidebar objective %q, want kills", sidebar)
	}
	if title.Text != "Kills" || title.Color != "red" {
		t.Errorf("objective title %+v, want Kills in red", title)
	}
	if prefix != "[A] " {
		t.Errorf("team prefix %q (%q), want [A] ", prefix, prefixColor)
	}
}
