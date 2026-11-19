package levelgen

// FortressPieceSave is a fortress piece as a chunk saves it under
// structures.starts. vanilla: StructurePiece.createTag, NetherFortressPieces
type FortressPieceSave struct {
	// ID is the piece type's registry id, such as "minecraft:nebs".
	ID string
	BB BoundingBox
	// Orientation is Direction.get2DDataValue: 0 south, 1 west, 2 north, 3 east.
	Orientation int
	GenDepth    int
	// Flag names the piece's own boolean ("Mob" or "Chest"), or is empty.
	Flag      string
	FlagValue bool
	// Seed is the end filler's "Seed" tag, present when HasSeed.
	Seed    int32
	HasSeed bool
}

var fortressPieceIDs = [...]string{
	pBridgeStraight:        "minecraft:nebs",
	pBridgeCrossing:        "minecraft:nebcr",
	pRoomCrossing:          "minecraft:nerc",
	pStairsRoom:            "minecraft:nesr",
	pMonsterThrone:         "minecraft:nemt",
	pCastleEntrance:        "minecraft:nece",
	pSmallCorridor:         "minecraft:nesc",
	pSmallCorridorCrossing: "minecraft:nescsc",
	pRightTurn:             "minecraft:nescrt",
	pLeftTurn:              "minecraft:nesclt",
	pCorridorStairs:        "minecraft:neccs",
	pTBalcony:              "minecraft:nectb",
	pStalkRoom:             "minecraft:necsr",
	pEndFiller:             "minecraft:nebef",
}

// fortressOrientation maps fdir (N, E, S, W) to Direction.get2DDataValue.
var fortressOrientation = [4]int{2, 3, 0, 1}

// SavedPieces returns the pieces as they stand now: a monster throne's
// spawner and a turn's chest are saved as placed once their chunk was written.
func (s *FortressStart) SavedPieces() []FortressPieceSave {
	out := make([]FortressPieceSave, len(s.pieces))
	for i, p := range s.pieces {
		sp := FortressPieceSave{ID: fortressPieceIDs[p.kind], BB: p.bb, Orientation: fortressOrientation[p.dir], GenDepth: p.depth}
		switch p.kind {
		case pMonsterThrone:
			sp.Flag, sp.FlagValue = "Mob", p.placed.Load()
		case pRightTurn, pLeftTurn:
			sp.Flag, sp.FlagValue = "Chest", p.needsChest && !p.placed.Load()
		case pEndFiller:
			sp.Seed, sp.HasSeed = p.selfSeed, true
		}
		out[i] = sp
	}
	return out
}
