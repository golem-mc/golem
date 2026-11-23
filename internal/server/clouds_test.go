package server

import (
	"bytes"
	"testing"

	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/nbt"
	"github.com/golem-mc/golem/registry"
	"github.com/google/uuid"
)

// TestCloudOfTagUpgradesLegacyBlockParticle verifies that disk-loaded area
// effect clouds upgrade pre-26.3 {Name, Properties} block_state tags in
// custom_particle, while command-supplied clouds stay on the 26.3 codec.
// vanilla: AreaEffectCloud.readAdditionalSaveData, BlockStateFieldNamesFix
func TestCloudOfTagUpgradesLegacyBlockParticle(t *testing.T) {
	t.Parallel()
	s := &Server{}
	uid := uuid.New()
	saved := nbt.Compound{
		"Pos":  nbt.List{Elem: nbt.TypeDouble, Items: []nbt.Tag{nbt.Double(0.5), nbt.Double(70), nbt.Double(0.5)}},
		"UUID": nbt.UUIDToIntArray(uid),
		"custom_particle": nbt.Compound{
			"type": nbt.String("minecraft:block"),
			"block_state": nbt.Compound{
				"Name":       nbt.String("minecraft:oak_log"),
				"Properties": nbt.Compound{"axis": nbt.String("x")},
			},
		},
	}

	cloud := s.cloudOfTag(nil, saved)
	if cloud == nil {
		t.Fatal("cloudOfTag returned nil")
	}
	if cloud.customParticle == nil {
		t.Fatal("saved cloud dropped its legacy block particle")
	}
	gotID, gotData, ok := registry.ParticleOptions(cloud.customParticle)
	if !ok {
		t.Fatalf("upgraded customParticle %#v rejected by ParticleOptions", cloud.customParticle)
	}
	wantID, _, _ := registry.ParticleID("minecraft:block")
	wantState, _ := registry.FindState("minecraft:oak_log", map[string]string{"axis": "x"})
	var wantBuf protocol.Buffer
	wantBuf.PutVarInt(int32(wantState))
	if gotID != wantID || !bytes.Equal(gotData, wantBuf.Bytes()) {
		t.Errorf("upgraded particle = (%d, % X), want (%d, % X)", gotID, gotData, wantID, wantBuf.Bytes())
	}

	// Command inputs do not upgrade legacy block_state tags.
	cmdCloud := s.buildCloudOfTag(nil, saved, false)
	if cmdCloud == nil {
		t.Fatal("buildCloudOfTag returned nil")
	}
	if cmdCloud.customParticle != nil {
		t.Errorf("command cloud kept legacy block particle %#v, want nil", cmdCloud.customParticle)
	}
}
