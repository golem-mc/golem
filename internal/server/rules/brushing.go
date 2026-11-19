package rules

import "github.com/golem-mc/golem/registry"

// The brush's numbers. vanilla: BrushItem, BrushableBlockEntity
const (
	// BrushUseDuration is the ticks one hold of the brush lasts.
	BrushUseDuration = 200
	// BrushStrokeTicks is the stroke period; the brush lands on the fifth
	// tick of each.
	BrushStrokeTicks = 10
	brushStrokeAt    = 5
	// BrushesToBreak is the brushes that finish a brushable block.
	BrushesToBreak    = 10
	brushResetTicks   = 40
	brushRetractTicks = 4
	brushRetractCount = 2
)

// BrushableBlock is what a brushable block turns into once brushed through
// and the sounds it makes.
type BrushableBlock struct {
	TurnsInto registry.Block
	Sound     registry.Sound
}

var brushableBlocks = map[registry.Block]BrushableBlock{
	registry.BlockSuspiciousSand:   {registry.BlockSand, registry.SoundItemBrushBrushingSand},
	registry.BlockSuspiciousGravel: {registry.BlockGravel, registry.SoundItemBrushBrushingGravel},
}

// BrushableBlockOf describes b when it is a brushable block.
func BrushableBlockOf(b registry.Block) (BrushableBlock, bool) {
	d, ok := brushableBlocks[b]
	return d, ok
}

// BrushSoundGeneric is the stroke's sound over a block that is not
// brushable. vanilla: SoundEvents.BRUSH_GENERIC
const BrushSoundGeneric = registry.SoundItemBrushBrushingGeneric

// BrushStrokeDue reports whether the tick, counted from 1 at the start of
// a hold, is one a stroke lands on. vanilla: BrushItem.onUseTick
func BrushStrokeDue(elapsed int) bool {
	return elapsed%BrushStrokeTicks == brushStrokeAt
}

// BrushHoldOver reports whether the hold has run out by the tick. vanilla:
// BrushItem.onUseTick (ticksRemaining < 0)
func BrushHoldOver(elapsed int) bool {
	return BrushUseDuration-elapsed+1 < 0
}

// DustedStage is the dusted block state property a brush count shows.
// vanilla: BrushableBlockEntity.getCompletionState
func DustedStage(count int) int {
	switch {
	case count == 0:
		return 0
	case count < 3:
		return 1
	case count < 6:
		return 2
	}
	return 3
}

// BrushProgress is the brushing a brushable block has taken, kept while it
// is being brushed and forgotten when it has recovered. Face is the face
// first brushed, -1 for none yet.
type BrushProgress struct {
	Count        int
	ResetsAt     int64
	CooldownEnds int64
	Face         int32
}

// NewBrushProgress is a block no one has brushed.
func NewBrushProgress() *BrushProgress { return &BrushProgress{Face: -1} }

// BrushStep is what one brush did. Counted is false for a brush the
// cooldown refused; Done says the tenth brush landed.
type BrushStep struct {
	Counted  bool
	Done     bool
	From, To int
}

// Brush takes a brush on the face at gameTime.
// vanilla: BrushableBlockEntity.brush
func (p *BrushProgress) Brush(gameTime int64, face int32) BrushStep {
	if p.Face < 0 {
		p.Face = face
	}
	p.ResetsAt = gameTime + brushResetTicks
	if gameTime < p.CooldownEnds {
		return BrushStep{}
	}
	p.CooldownEnds = gameTime + BrushStrokeTicks
	from := DustedStage(p.Count)
	p.Count++
	if p.Count >= BrushesToBreak {
		return BrushStep{Counted: true, Done: true, From: from, To: from}
	}
	return BrushStep{Counted: true, From: from, To: DustedStage(p.Count)}
}

// Recover lets the block recover from brushing: it reports the stages it
// moved between and whether the block still has brushing to recover from.
// vanilla: BrushableBlockEntity.checkReset
func (p *BrushProgress) Recover(gameTime int64) (from, to int, active bool) {
	from = DustedStage(p.Count)
	to = from
	if p.Count != 0 && gameTime >= p.ResetsAt {
		p.Count = max(0, p.Count-brushRetractCount)
		to = DustedStage(p.Count)
		p.ResetsAt = gameTime + brushRetractTicks
	}
	if p.Count == 0 {
		p.Face, p.ResetsAt, p.CooldownEnds = -1, 0, 0
		return from, to, false
	}
	return from, to, true
}
