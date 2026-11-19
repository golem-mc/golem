package entity

// mayConvert asks the server whether the body may turn into into. A refused
// conversion is abandoned, so it is not asked again every tick: the timer
// that led to it starts over.
func (m *Mob) mayConvert(into MobType) bool {
	if m.Convert == nil || m.Convert(m, into) {
		return true
	}
	switch {
	case m.convertsInWater():
		m.InWaterTime = -1
		m.SetMetadataDrownedConverting(false)
	case m.Type == TypeSkeleton:
		m.inPowderSnowTime = strayNotConvertingTime
		m.SetMetadataStrayConverting(false)
	case m.Type == TypeZombieVillager:
		m.resetCure()
	case m.Type == TypeHoglin:
		m.hoglin.inOverworld = 0
	case m.isPiglin():
		m.barter.inOverworld = 0
	case m.Type == TypeTadpole:
		m.gone = false
		m.tadpole.age = 0
	}
	return false
}
