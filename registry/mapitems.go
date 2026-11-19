package registry

// mapItems marks the MapItems: the filled map and the explorer maps. No tag
// names them all, as #clonable_maps is a data pack's to change.
// vanilla: Items.FILLED_MAP ... Items.WARM_OCEAN_RUINS_MAP (MapItem::new)
var mapItems = func() (t [ItemCount]bool) {
	for _, i := range []Item{
		ItemFilledMap, ItemOceanMonumentMap, ItemWoodlandMansionMap, ItemBuriedTrialChambersMap,
		ItemJunglePyramidMap, ItemSwampHutMap, ItemDesertVillageMap, ItemPlainsVillageMap,
		ItemSavannaVillageMap, ItemSnowyVillageMap, ItemTaigaVillageMap, ItemBuriedTreasureMap,
		ItemBuriedAncientCityMap, ItemBuriedMineshaftMap, ItemDesertPyramidMap, ItemAbandonedCampMap,
		ItemWarmOceanRuinsMap,
	} {
		t[i] = true
	}
	return t
}()

// IsMap reports whether the item is a MapItem, false for an ID outside the
// registry.
func (i Item) IsMap() bool { return i >= 0 && i < ItemCount && mapItems[i] }
