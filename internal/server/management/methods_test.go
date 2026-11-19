package management

import "testing"

// notServedYet are the schema's methods no issue has implemented; it only
// shrinks.
var notServedYet = map[string]bool{}

func TestServedMethodsMatchTheSchema(t *testing.T) {
	t.Parallel()
	s := &Server{}
	served := s.served()
	for name, param := range methodParams {
		m, ok := served[name]
		switch {
		case notServedYet[name] && ok:
			t.Errorf("%s is served but listed in notServedYet", name)
		case !notServedYet[name] && !ok:
			t.Errorf("%s is in the schema but not served", name)
		case ok && m.param != param:
			t.Errorf("%s takes param %q, the schema says %q", name, m.param, param)
		}
	}
	for name := range served {
		if _, ok := methodParams[name]; !ok && name != "minecraft:rpc.discover" {
			t.Errorf("%s is served but not in the schema", name)
		}
	}
}
