package protocol

// ComponentChecksumOf is the checksum a hashed claim carries for one
// component of the stack: the integer the codec-hash's four digest bytes
// make. The false comes back for a component this server cannot hash — an
// enchantment whose name the lookup does not carry — and a claim carrying
// one is corrected rather than confirmed, the safe answer either way.
func ComponentChecksumOf(s ItemStack, component int32) (int32, bool) {
	digest, ok := componentChecksumOf(s, component)
	if !ok {
		return 0, false
	}
	return int32Le(digest), true
}
