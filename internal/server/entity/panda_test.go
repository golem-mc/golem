package entity

import (
	"testing"

	"github.com/golem-mc/golem/nbt"
)

func TestPandaVariantFromGenes(t *testing.T) {
	t.Parallel()
	cases := []struct{ main, hidden, want PandaGene }{
		{GeneBrown, GeneNormal, GeneNormal},
		{GeneBrown, GeneBrown, GeneBrown},
		{GeneWeak, GeneBrown, GeneNormal},
		{GeneLazy, GeneNormal, GeneLazy},
		{GeneAggressive, GeneBrown, GeneAggressive},
	}
	for _, c := range cases {
		if got := pandaGeneVariant(c.main, c.hidden); got != c.want {
			t.Errorf("(%s, %s) = %s, want %s", c.main.Name(), c.hidden.Name(), got.Name(), c.want.Name())
		}
	}
}

func TestPandaGenesSaveAndSetAttributes(t *testing.T) {
	t.Parallel()
	m := NewMob(TypePanda, 1, 0, 64, 0, 1)
	m.LoadFrom(nbt.Compound{"MainGene": nbt.String("weak"), "HiddenGene": nbt.String("weak")}, nil, nil)
	if m.MaxHealth != 10 || m.Health > 10 {
		t.Fatalf("weak panda health %v/%v", m.Health, m.MaxHealth)
	}
	saved := nbt.Compound{}
	m.SaveTo(saved, nil)
	if saved["MainGene"] != nbt.String("weak") || saved["HiddenGene"] != nbt.String("weak") {
		t.Fatalf("saved genes %v %v", saved["MainGene"], saved["HiddenGene"])
	}
	m.LoadFrom(nbt.Compound{"MainGene": nbt.String("lazy"), "HiddenGene": nbt.String("bogus")}, nil, nil)
	if m.PandaHiddenGene() != GeneNormal || m.paceBase() != float64(float32(0.07)) {
		t.Fatalf("lazy panda hidden %v pace %v", m.PandaHiddenGene(), m.paceBase())
	}
}
