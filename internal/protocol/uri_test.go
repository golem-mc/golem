package protocol

import "testing"

// TestUntrustedURIMatchesJavaURI pins the open_url check to java.net.URI: a
// bracket is legal in the query, fragment and opaque parts but a bracket host
// must be an IPv6 literal, one '#' opens the fragment and the part before it
// must not be empty, and a non-ASCII space or control is refused.
// vanilla: ExtraCodecs.UNTRUSTED_URI, Util.parseAndValidateUntrustedUri
func TestUntrustedURIMatchesJavaURI(t *testing.T) {
	t.Parallel()
	accepted := []string{
		"http://a?[b]",
		"http://a/b#[c]",
		"http:[abc]",
		"https://x.com/?a[]=1",
		"http://[::1]/",
		"http://[::01.2.3.4]",
		"http://[::001.2.3.4]",
		"http://[::1.02.3.4]",
	}
	for _, u := range accepted {
		if !ValidUntrustedURI(u) {
			t.Errorf("ValidUntrustedURI(%q) = false, want accepted", u)
		}
	}
	refused := []string{
		"http://a#b#c",
		"http://a##",
		"http:#",
		"http:#x",
		"http://a\u00a0b",
		"http://a\u0085b",
		"http://a\u3000b",
		"http://[abc]/",
		"http://a[b]/",
		"http://[::1.2.3.256]",
		"http://a@@[::1]",
		"https://a:b@@[::1]:80",
		"http://auser@@[::1]",
	}
	for _, u := range refused {
		if ValidUntrustedURI(u) {
			t.Errorf("ValidUntrustedURI(%q) = true, want refused", u)
		}
	}
}
