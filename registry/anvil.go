package registry

// DamageAnvilState is the state an anvil becomes after one step of damage,
// keeping its facing; gone means a damaged anvil broke. A state that is no
// anvil is returned unchanged.
// vanilla: AnvilBlock.damage
func DamageAnvilState(state uint16) (next uint16, gone bool) {
	var block Block
	switch BlockOf(state) {
	case BlockAnvil:
		block = BlockChippedAnvil
	case BlockChippedAnvil:
		block = BlockDamagedAnvil
	case BlockDamagedAnvil:
		return 0, true
	default:
		return state, false
	}
	next, ok := WithProps(block.DefaultState(), PropValue{PropFacing, StateValue(state, PropFacing)})
	return next, !ok
}
