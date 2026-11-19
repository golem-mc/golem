package effect

// Cause is what changed a body's effect, as the plugin events report it.
// The zero value is Other, so a caller that sets none is read as that.
type Cause uint8

const (
	CauseOther Cause = iota
	CausePotion
	CauseSplashPotion
	CauseAreaCloud
	CauseArrow
	CauseFood
	CauseMilk
	CauseBeacon
	CauseConduit
	CauseAttack
	CauseCommand
	CauseExpired
	CauseTotem
	CausePlugin
)
