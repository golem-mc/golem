package registry

// The piston tables again, indexed by Block, so a state classifies with
// BlockOf and one array read.
var (
	PistonDestroyOnPushBlocks  = BlocksWhere(func(n string) bool { return PistonDestroyOnPush[n] })
	PistonImmovableBlocks      = BlocksWhere(func(n string) bool { return PistonImmovable[n] })
	PistonPushOnlyBlocks       = BlocksWhere(func(n string) bool { return PistonPushOnly[n] })
	PistonUnbreakableBlocks    = BlocksWhere(func(n string) bool { return PistonUnbreakable[n] })
	PistonHasBlockEntityBlocks = BlocksWhere(func(n string) bool { return PistonHasBlockEntity[n] })
)
