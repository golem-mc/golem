package command

import (
	"bytes"
	"strings"
	"testing"

	"github.com/golem-mc/golem/nbt"
	"github.com/golem-mc/golem/registry"
)

func TestParticleOptionsAcceptOrderedCompounds(t *testing.T) {
	for _, text := range []string{
		`{type:"minecraft:smoke"}`,
		`{type:"minecraft:block",block_state:{Name:"minecraft:furnace",Properties:{facing:"east",lit:"true"}}}`,
		`{type:"minecraft:item",item:{id:"minecraft:stone",count:3}}`,
		`{type:"minecraft:vibration",destination:{type:"block",pos:[1,64,-3]},arrival_in_ticks:20}`,
		`{type:"minecraft:trail",target:[1.5d,64,-2],color:255,duration:40}`,
	} {
		t.Run(text, func(t *testing.T) {
			plain, err := nbt.ParseSNBT(text)
			if err != nil {
				t.Fatal(err)
			}
			wantID, wantData, ok := ParticleOptions(plain)
			if !ok {
				t.Fatal("plain particle options rejected")
			}
			id, data, ok := ParticleOptions(nbt.OrderedCopy(plain))
			if !ok || id != wantID || !bytes.Equal(data, wantData) {
				t.Fatalf("ordered options = (%d, %x, %t), want (%d, %x, true)", id, data, ok, wantID, wantData)
			}
		})
	}
}

// TestParticleOptionsCoverEveryTypeWithData checks that no particle type
// that carries data falls through to the missing-reader error.
func TestParticleOptionsCoverEveryTypeWithData(t *testing.T) {
	for _, name := range registry.ParticleTypes {
		if _, withData, _ := registry.ParticleID(name); !withData {
			continue
		}
		if _, err := particleOptions(name, nbt.Compound{}); err != nil && strings.HasSuffix(err.Error(), "has no option reader") {
			t.Errorf("%s has no option reader", name)
		}
	}
}
