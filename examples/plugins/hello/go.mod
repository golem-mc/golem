module example.com/hello

go 1.27.4

require github.com/golem-mc/golem v0.2.0

require (
	github.com/google/uuid v1.6.0 // indirect
	github.com/klauspost/compress v1.20.5 // indirect
	github.com/magiconair/properties v1.18.13 // indirect
	github.com/pierrec/lz4/v4 v4.1.34 // indirect
	go.yaml.in/yaml/v4 v4.0.0 // indirect
	golang.org/x/sys v0.50.0 // indirect
	golang.org/x/term v0.48.0 // indirect
)

// Builds against this checkout; a released plugin requires a server version
// instead and has no replace.
replace github.com/golem-mc/golem => ../../..
