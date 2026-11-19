package inventory

// The buttons of the lectern window; a button from LecternButtonPageJump on
// jumps to page button-100.
// vanilla: LecternMenu.clickMenuButton
const (
	LecternButtonPrevPage = 1
	LecternButtonNextPage = 2
	LecternButtonTakeBook = 3
	LecternButtonPageJump = 100
)

// LecternAction is what a lectern button asks of the block.
type LecternAction int

const (
	// LecternNothing is a button the menu does not know, or a take by a
	// body that may not build.
	LecternNothing LecternAction = iota
	// LecternTurn asks the book to open at the returned page.
	LecternTurn
	// LecternTake asks for the book to go to the player.
	LecternTake
)

// LecternPress decides a press on a lectern window button. page is the page
// the book is open at; the target is unclamped, the book clamps it.
// vanilla: LecternMenu.clickMenuButton
func LecternPress(button int32, page int, mayBuild bool) (target int, action LecternAction) {
	switch {
	case button >= LecternButtonPageJump:
		return int(button - LecternButtonPageJump), LecternTurn
	case button == LecternButtonNextPage:
		return page + 1, LecternTurn
	case button == LecternButtonPrevPage:
		return page - 1, LecternTurn
	case button == LecternButtonTakeBook && mayBuild:
		return page, LecternTake
	}
	return page, LecternNothing
}
