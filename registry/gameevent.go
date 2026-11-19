package registry

var gameEventsByName = func() map[string]GameEvent {
	ids := make(map[string]GameEvent, GameEventCount-1)
	for id := 1; id < GameEventCount; id++ {
		ids[gameEventNames[id]] = GameEvent(id)
	}
	return ids
}()

// GameEventByName resolves a qualified registry key at a serialization boundary, or returns GameEventNone.
func GameEventByName(name string) GameEvent { return gameEventsByName[name] }

// Name returns the event's registry key, or empty for an unregistered event.
func (e GameEvent) Name() string {
	if int(e) >= len(gameEventNames) {
		return ""
	}
	return gameEventNames[e]
}
