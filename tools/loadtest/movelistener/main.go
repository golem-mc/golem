// Command movelistener is golem with a plugin that listens to every
// PlayerMoveEvent and does nothing, the load an AFK-detecting plugin such as
// Essentials puts on a moving player's connection.
package main

import (
	"github.com/golem-mc/golem/app"
	"github.com/golem-mc/golem/plugin"
)

type moveListener struct{}

func (moveListener) Enable(s plugin.Server) error {
	plugin.Listen(s, func(*plugin.PlayerMoveEvent) {})
	return nil
}

func (moveListener) Disable() {}

func main() {
	plugin.Register("movelistener", moveListener{})
	app.Main()
}
