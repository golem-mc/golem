package registry

import (
	"bytes"
	"testing"

	"github.com/golem-mc/golem/nbt"
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
