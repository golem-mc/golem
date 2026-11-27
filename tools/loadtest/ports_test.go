package main

import "testing"

func TestFreePortsDistinct(t *testing.T) {
	for range 1000 {
		ports, err := freePorts(3)
		if err != nil {
			t.Fatal(err)
		}
		if len(ports) != 3 || ports[0] == ports[1] || ports[0] == ports[2] || ports[1] == ports[2] {
			t.Fatalf("ports %v", ports)
		}
	}
}

func TestPickPortsKeepsSetPorts(t *testing.T) {
	o := &options{port: 25565}
	if err := o.pickPorts(); err != nil {
		t.Fatal(err)
	}
	if o.port != 25565 || o.rconPort == 0 || o.pprofPort == 0 || o.rconPort == o.pprofPort || o.rconPort == o.port {
		t.Fatalf("%+v", o)
	}
}
