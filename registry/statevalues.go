package registry

// The creaking heart's own properties.
var (
	PropCreakingHeartState = mustProp("creaking_heart_state")
	PropNatural            = mustProp("natural")
	PropTip                = mustProp("tip")
)

// The bookshelf's six cells, and the brewing stand's three bottle slots,
// read and written together.
var (
	BookshelfSlotProps = [6]Prop{mustProp("slot_0_occupied"), mustProp("slot_1_occupied"), mustProp("slot_2_occupied"), mustProp("slot_3_occupied"), mustProp("slot_4_occupied"), mustProp("slot_5_occupied")}
	BrewingBottleProps = [3]Prop{mustProp("has_bottle_0"), mustProp("has_bottle_1"), mustProp("has_bottle_2")}
)

// The properties the engine reads on hot paths, beside the ones stateset.go
// declares for its setters.
var (
	PropAttachment        = mustProp("attachment")
	PropAxis              = mustProp("axis")
	PropBerries           = mustProp("berries")
	PropBites             = mustProp("bites")
	PropBloom             = mustProp("bloom")
	PropBottom            = mustProp("bottom")
	PropCandles           = mustProp("candles")
	PropCanSummon         = mustProp("can_summon")
	PropCharges           = mustProp("charges")
	PropConditional       = mustProp("conditional")
	PropCracked           = mustProp("cracked")
	PropCrafting          = mustProp("crafting")
	PropDelay             = mustProp("delay")
	PropDusted            = mustProp("dusted")
	PropDisarmed          = mustProp("disarmed")
	PropDrag              = mustProp("drag")
	PropEggs              = mustProp("eggs")
	PropEye               = mustProp("eye")
	PropFace              = mustProp("face")
	PropFlowerAmount      = mustProp("flower_amount")
	PropHalf              = mustProp("half")
	PropHanging           = mustProp("hanging")
	PropHasBook           = mustProp("has_book")
	PropHasRecord         = mustProp("has_record")
	PropHatch             = mustProp("hatch")
	PropHinge             = mustProp("hinge")
	PropHoneyLevel        = mustProp("honey_level")
	PropHydration         = mustProp("hydration")
	PropInWall            = mustProp("in_wall")
	PropInstrument        = mustProp("instrument")
	PropInverted          = mustProp("inverted")
	PropLayers            = mustProp("layers")
	PropLeaves            = mustProp("leaves")
	PropMoisture          = mustProp("moisture")
	PropNote              = mustProp("note")
	PropOccupied          = mustProp("occupied")
	PropOminous           = mustProp("ominous")
	PropOpen              = mustProp("open")
	PropOrientation       = mustProp("orientation")
	PropPickles           = mustProp("pickles")
	PropPersistent        = mustProp("persistent")
	PropPart              = mustProp("part")
	PropRotation          = mustProp("rotation")
	PropSculkSensorPhase  = mustProp("sculk_sensor_phase")
	PropShape             = mustProp("shape")
	PropSegmentAmount     = mustProp("segment_amount")
	PropSideChain         = mustProp("side_chain")
	PropShrieking         = mustProp("shrieking")
	PropSnowy             = mustProp("snowy")
	PropStage             = mustProp("stage")
	PropThickness         = mustProp("thickness")
	PropTilt              = mustProp("tilt")
	PropTrialSpawnerState = mustProp("trial_spawner_state")
	PropUnstable          = mustProp("unstable")
	PropVaultState        = mustProp("vault_state")
	PropVerticalDirection = mustProp("vertical_direction")
)

// The value table covers every Prop and is two-level because states share
// far fewer rows of values (about 6.5k) than there are states (about 32k):
// stateValueRow maps a state to its row, and valueRows holds valueWidth value
// indexes per row, one per Prop. A value index names a string in valueNames,
// interned across every property; index 0 is "", a property the state does
// not have.
var (
	stateValueRow []uint16
	valueRows     []uint8
	valueWidth    int
	valueNames    []string
	valueInts     []int8 // a value's number, -1 for one that is not a number
	valueTrue     uint8
)

func init() {
	valueWidth = len(stateTables.names)
	stateValueRow = make([]uint16, len(stateByID))
	empty := make([]uint8, valueWidth)
	rowOf := map[string]uint16{string(empty): 0}
	valueRows = append([]uint8(nil), empty...)
	valueOf := map[string]uint8{"": 0}
	valueNames = []string{""}
	valueInts = []int8{-1}
	intern := func(v string) uint8 {
		if i, ok := valueOf[v]; ok {
			return i
		}
		if len(valueNames) > 0xff {
			panic("registry: more than 255 block-state property values")
		}
		i := uint8(len(valueNames))
		valueOf[v] = i
		valueNames = append(valueNames, v)
		n := -1
		if v != "" && len(v) <= 2 {
			n = 0
			for _, c := range v {
				if c < '0' || c > '9' {
					n = -1
					break
				}
				n = n*10 + int(c-'0')
			}
		}
		valueInts = append(valueInts, int8(n))
		return i
	}
	valueTrue = intern("true")
	byProps := map[string]uint16{"": 0}
	buf := make([]uint8, valueWidth)
	for id := range stateByID {
		info := &stateByID[id]
		row, ok := byProps[info.props]
		if !ok {
			clear(buf)
			for k, v := range info.parsed {
				buf[stateTables.byName[k]] = intern(v)
			}
			if row, ok = rowOf[string(buf)]; !ok {
				if len(valueRows)/valueWidth > 0xffff {
					panic("registry: more block-state value rows than a uint16 holds")
				}
				row = uint16(len(valueRows) / valueWidth)
				rowOf[string(buf)] = row
				valueRows = append(valueRows, buf...)
			}
			byProps[info.props] = row
		}
		stateValueRow[id] = row
	}
}

// stateValueIndex is the state's value index for p, 0 for a state without
// the property and for an ID that is no state.
func stateValueIndex(id uint16, p Prop) uint8 {
	if int(id) >= len(stateValueRow) {
		return 0
	}
	return valueRows[int(stateValueRow[id])*valueWidth+int(p)]
}

// StateValue returns the state's value of p as the block states spell it, ""
// for a state without the property: StateProps(id)[p.Name()] without the
// map.
func StateValue(id uint16, p Prop) string { return valueNames[stateValueIndex(id, p)] }

// StateHas reports whether the state has the property p.
func StateHas(id uint16, p Prop) bool { return stateValueIndex(id, p) != 0 }

// StateBool reports whether the state's p is "true"; false for a state
// without it.
func StateBool(id uint16, p Prop) bool { return stateValueIndex(id, p) == valueTrue }

// StateInt returns the state's p as a number; ok is false for a state without
// the property and for a value that is not a number.
func StateInt(id uint16, p Prop) (n int, ok bool) {
	n = int(valueInts[stateValueIndex(id, p)])
	return n, n >= 0
}
