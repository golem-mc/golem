package rules

import "github.com/golem-mc/golem/registry"

// CopperStatueClick is what a click with an item does to a copper golem
// statue.
type CopperStatueClick int

// The results of CopperStatueClickOf.
const (
	CopperStatuePass   CopperStatueClick = iota // left to the item's own use
	CopperStatueRevive                          // the statue becomes a golem again
	CopperStatuePose                            // the statue steps to its next pose
)

// nextCopperGolemPose is the pose a click steps a statue to.
var nextCopperGolemPose = map[string]string{"standing": "sitting", "sitting": "running", "running": "star", "star": "standing"}

// CopperStatueClickOf decides a click on a copper golem statue state: an axe
// on an unwaxed, unweathered statue revives the golem; any other item but an
// axe or, on an unwaxed statue, honeycomb steps the pose, returning the
// stepped state.
// vanilla: CopperGolemStatueBlock.useItemOn, WeatheringCopperGolemStatueBlock.useItemOn
func CopperStatueClickOf(state uint16, held registry.Item) (CopperStatueClick, uint16) {
	block := registry.BlockOf(state)
	if !block.InGroup(registry.GroupCopperGolemStatue) {
		return CopperStatuePass, state
	}
	_, waxed := block.Unwaxed()
	_, weathers := block.Scraped()
	if held.InTag(registry.ItemTagIDAxes) {
		if waxed || weathers {
			return CopperStatuePass, state
		}
		return CopperStatueRevive, state
	}
	if !waxed && held == registry.ItemHoneycomb {
		return CopperStatuePass, state
	}
	next, ok := registry.WithProp(state, registry.PropCopperGolemPose, nextCopperGolemPose[registry.StateValue(state, registry.PropCopperGolemPose)])
	if !ok {
		return CopperStatuePass, state
	}
	return CopperStatuePose, next
}
