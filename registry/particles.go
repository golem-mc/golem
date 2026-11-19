package registry

var particleIDs = func() map[string]int32 {
	ids := make(map[string]int32, len(ParticleTypes))
	for i, name := range ParticleTypes {
		ids[name] = int32(i)
	}
	return ids
}()

// ParticleID reports the ID of the named particle type and whether the
// type carries data of its own.
func ParticleID(name string) (id int32, withData, ok bool) {
	id, ok = particleIDs[name]
	return id, ok && particlesWithData[name], ok
}
