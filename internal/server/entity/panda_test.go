package entity

import (
	"math/rand"
	"testing"

	"github.com/golem-mc/golem/internal/server/rules"
	"github.com/golem-mc/golem/nbt"
)

func TestPandaRandomGeneTable(t *testing.T) {
	t.Parallel()
	want := [16]PandaGene{GeneLazy, GeneWorried, GenePlayful, GeneWeak, GeneAggressive, GeneWeak, GeneWeak, GeneWeak, GeneWeak,
		GeneBrown, GeneBrown, GeneNormal, GeneNormal, GeneNormal, GeneNormal, GeneNormal}
	// roll 3 is weak too: nextInt < 9 catches it after the explicit rolls
	for roll, gene := range want {
		if got := pandaRandomGene(roll); got != gene {
			t.Errorf("roll %d = %s, want %s", roll, got.Name(), gene.Name())
		}
	}
}

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

func TestPandaBambooSearchSpiral(t *testing.T) {
	t.Parallel()
	w := newTestWorld()
	m := NewMob(TypePanda, 1, 0.5, 64, 0.5, 1)
	for _, c := range []struct {
		x, y, z int32
		want    bool
	}{{7, 64, -7, true}, {-7, 66, 0, true}, {8, 64, 0, false}, {0, 67, 0, false}, {0, 63, 0, false}} {
		w.set(c.x, c.y, c.z, defaultState(t, "minecraft:bamboo"))
		if got := m.pandaFindsBamboo(w); got != c.want {
			t.Errorf("bamboo at %d,%d,%d found = %v, want %v", c.x, c.y, c.z, got, c.want)
		}
		w.set(c.x, c.y, c.z, 0)
	}
}

func TestPandaGroupMembersAfterTheFirstAreBabiesAtOneInFive(t *testing.T) {
	t.Parallel()
	group := NewSpawnGroup(GoDice{rand.New(rand.NewSource(9))})
	babies := 0
	for i := range 2000 {
		m := NewMob(TypePanda, int32(i+1), 0, 64, 0, int64(i))
		m.FinalizeSpawn(GoDice{m.Rand}, testBiome("minecraft:jungle"), group, rules.LocalDifficulty{})
		if m.IsBaby() {
			babies++
		}
	}
	if babies < 340 || babies > 460 {
		t.Fatalf("%d babies of 2000, want about 20%%", babies)
	}
}

func TestPandaInheritIntoRunsGeneInheritance(t *testing.T) {
	t.Parallel()
	parent := NewMob(TypePanda, 1, 0, 64, 0, 1)
	parent.panda.main, parent.panda.hidden = GeneWeak, GeneWeak
	weak := 0
	for i := range 200 {
		baby := NewMob(TypePanda, int32(i+2), 0, 64, 0, int64(i+2))
		parent.InheritInto(baby)
		if baby.panda.main == GeneWeak && baby.panda.hidden == GeneWeak {
			weak++
		}
	}
	if weak < 170 {
		t.Fatalf("%d of 200 weak/weak cubs, want about 94%%", weak)
	}
}
