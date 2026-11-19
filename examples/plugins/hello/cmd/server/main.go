// Command server is the golem server with the hello plugin built in:
// the plugin's init registers it before the server starts.
package main

import (
	_ "example.com/hello"
	"github.com/golem-mc/golem/app"
)

func main() { app.Main() }
