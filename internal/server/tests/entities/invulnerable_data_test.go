package entities

import (
	"testing"

	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/internal/server/servertest"
)

// dataGetIs reports whether a /data get answered with the given value.
func dataGetIs(packets []servertest.PushedPacket, want string) bool {
	for _, p := range packets {
		if p.Id != protocol.IDPlaySystemChat {
			continue
		}
		text, ok := protocol.ReadText(protocol.NewReader(p.Data))
		if ok && text.Translate == "commands.data.entity.query" && len(text.With) == 2 && text.With[1].Plain() == want {
			return true
		}
	}
	return false
}

// noGravityIn reads the last NoGravity field among packets' metadata.
func noGravityIn(t *testing.T, packets []servertest.PushedPacket) (value, sent bool) {
	t.Helper()
	for _, p := range packets {
		var rd *protocol.Reader
		switch p.Id {
		case protocol.IDPlayEntityMetadata:
			rd = protocol.NewReader(p.Data)
			rd.ReadVarInt()
		default:
			continue
		}
		entries, ok := servertest.ReadMetadata(rd)
		if !ok {
			t.Fatal("unreadable metadata")
		}
		if e := servertest.MetaOf(entries, protocol.MetadataIndexNoGravity); e != nil {
			value, sent = e.FlagVal, true
		}
	}
	return value, sent
}

// vanilla: Entity.load, Entity.saveWithoutId
func TestInvulnerableAndNoGravityRoundTrip(t *testing.T) {
	t.Parallel()
	srv, c := servertest.Join(t, servertest.MobTestConfig(), "Tester", servertest.OpsListing(t, "Tester"))
	servertest.HoldTicks(t, srv)
	r := servertest.NewMetadataRecorder(t, srv, c)
	r.Ticks = 0
	cases := []struct{ kind, extra, key string }{
		{"arrow", "", "Invulnerable"},
		{"snowball", "", "Invulnerable"},
		{"experience_orb", "", "Invulnerable"},
		{"falling_block", "", "Invulnerable"},
		{"tnt", "", "Invulnerable"},
		{"area_effect_cloud", ",Duration:10000", "Invulnerable"},
		{"item_frame", ",Fixed:1b", "NoGravity"},
		{"painting", ",variant:\"minecraft:kebab\"", "NoGravity"},
		{"area_effect_cloud", ",Duration:10000", "NoGravity"},
	}
	for _, tc := range cases {
		t.Run(tc.kind+"/"+tc.key, func(t *testing.T) {
			target := "@e[type=minecraft:" + tc.kind + ",limit=1]"
			spawn := r.Run("summon minecraft:" + tc.kind + " 4.5 70 4.5 {" + tc.key + ":1b" + tc.extra + "}")
			if tc.key == "NoGravity" {
				if got, ok := noGravityIn(t, spawn); !ok || !got {
					t.Errorf("spawn metadata NoGravity = %v (sent %v), want true", got, ok)
				}
				cleared := r.Run("data merge entity " + target + " {NoGravity:0b}")
				if got, ok := noGravityIn(t, cleared); !ok || got {
					t.Errorf("cleared metadata NoGravity = %v (sent %v), want an explicit false", got, ok)
				}
				r.Run("data merge entity " + target + " {NoGravity:1b}")
			}
			if !dataGetIs(r.Run("data get entity "+target+" "+tc.key), "1b") {
				t.Errorf("%s did not read back 1b", tc.key)
			}
			r.Run("kill " + target)
		})
	}
}
