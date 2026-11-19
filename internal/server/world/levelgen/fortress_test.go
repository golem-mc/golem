package levelgen

import "testing"

func TestRandomSpreadChunk(t *testing.T) {
	t.Parallel()
	for gz := int32(-3); gz <= 3; gz++ {
		for gx := int32(-3); gx <= 3; gx++ {
			sx, sz := RandomSpreadChunk(5, gx*NetherComplexSpacing, gz*NetherComplexSpacing,
				NetherComplexSpacing, NetherComplexSeparation, NetherComplexSalt)
			ox, oz := sx-gx*NetherComplexSpacing, sz-gz*NetherComplexSpacing
			if ox < 0 || ox >= NetherComplexSpacing-NetherComplexSeparation || oz < 0 || oz >= NetherComplexSpacing-NetherComplexSeparation {
				t.Fatalf("cell (%d, %d): start at (%d, %d) lies outside its spacing-separation square", gx, gz, sx, sz)
			}
		}
	}
}

// TestFortressStartBounds pins the bounding boxes of fortresses whose blocks
// were compared with a vanilla server by worldgen:parity.
func TestFortressStartBounds(t *testing.T) {
	t.Parallel()
	cases := []struct {
		seed   int64
		cx, cz int32
		want   BoundingBox
	}{
		{0, 15, 2, BoundingBox{157, 48, 26, 356, 81, 154}},
		{0, 4, -36, BoundingBox{66, 54, -581, 192, 69, -453}},
		{12345, 6, -32, BoundingBox{20, 48, -535, 200, 69, -389}},
	}
	for _, c := range cases {
		sx, sz := RandomSpreadChunk(c.seed, floorDiv(c.cx, NetherComplexSpacing)*NetherComplexSpacing,
			floorDiv(c.cz, NetherComplexSpacing)*NetherComplexSpacing,
			NetherComplexSpacing, NetherComplexSeparation, NetherComplexSalt)
		if sx != c.cx || sz != c.cz {
			t.Fatalf("seed %d: placement chunk (%d, %d), want (%d, %d)", c.seed, sx, sz, c.cx, c.cz)
		}
		if !NetherComplexIsFortress(c.seed, sx, sz, true) {
			t.Fatalf("seed %d chunk (%d, %d) is not a fortress", c.seed, sx, sz)
		}
		if got := NewFortressStart(c.seed, sx, sz).Bounds; got != c.want {
			t.Errorf("seed %d chunk (%d, %d): bounds %+v, want %+v", c.seed, sx, sz, got, c.want)
		}
	}
}

func TestNetherComplexBastionBiome(t *testing.T) {
	t.Parallel()
	// A start the weighted pick gives to the bastion is a fortress only
	// where the bastion cannot generate.
	var bastion, fortress int
	for cx := range int32(200) {
		if NetherComplexIsFortress(7, cx, 3, true) {
			fortress++
		} else {
			bastion++
		}
		if !NetherComplexIsFortress(7, cx, 3, false) {
			t.Fatalf("chunk %d: a start with an invalid bastion biome must be a fortress", cx)
		}
	}
	if fortress == 0 || bastion == 0 || fortress >= bastion {
		t.Fatalf("fortress %d, bastion %d of 200: want the fortress's 2 in 5 share", fortress, bastion)
	}
}

// recordingWorld notes every block a fortress writes.
type recordingWorld struct {
	writes map[[3]int]uint16
	bad    [][3]int
	chunk  BoundingBox
}

func (w *recordingWorld) Block(x, y, z int) uint16 { return w.writes[[3]int{x, y, z}] }
func (w *recordingWorld) SetBlock(x, y, z int, s uint16) {
	if !w.chunk.inside(x, y, z) {
		w.bad = append(w.bad, [3]int{x, y, z})
	}
	w.writes[[3]int{x, y, z}] = s
}
func (w *recordingWorld) ReplaceableByStructures(s uint16) bool { return s == 0 }
func (w *recordingWorld) SolidRender(s uint16) bool             { return s != 0 }
func (w *recordingWorld) IsChest(uint16) bool                   { return false }
func (w *recordingWorld) PlaceChest(x, y, z, _ int)             { w.SetBlock(x, y, z, 999) }
func (w *recordingWorld) PlaceSpawner(x, y, z int)              { w.SetBlock(x, y, z, 998) }
func (w *recordingWorld) FenceAt(int, int, int)                 {}
func (w *recordingWorld) ScheduleFluid(int, int, int)           {}

func TestFortressMirrorAndRotation(t *testing.T) {
	t.Parallel()
	// An east fence on a piece facing south is a west fence, facing west a
	// south fence, facing east a south fence too: mirror first, then rotate.
	cases := []struct {
		dir  fdir
		in   uint8
		want uint8
	}{
		{dN, fE, fE},
		{dS, fN, fS},
		{dS, fE, fE},
		{dW, fN, fW},
		{dW, fE, fS},
		{dE, fN, fE},
		{dE, fE, fS},
	}
	for _, c := range cases {
		p := &fortressPiece{dir: c.dir}
		if got := p.transform(fenceB(c.in)); got.arg != c.want {
			t.Errorf("facing %d: fence %04b became %04b, want %04b", c.dir, c.in, got.arg, c.want)
		}
	}
	p := &fortressPiece{dir: dW}
	if got := p.transform(stairsB(dN)); got.arg != uint8(dW) {
		t.Errorf("stairs facing north on a west piece face %d, want west", got.arg)
	}
}
