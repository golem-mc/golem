// Command golem runs the server with no plugins; the launcher builds
// its own main package that imports the plugins first.
package main

import "github.com/golem-mc/golem/app"

func main() { app.Main() }
