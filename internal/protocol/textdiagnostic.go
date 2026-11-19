package protocol

import (
	"strconv"
	"strings"
	"unicode"
	"unicode/utf8"
)

// UUIDStringMalformed returns UUID.fromString's exception message, or "".
// vanilla: UUIDUtil.STRING_CODEC, UUID.fromString
func UUIDStringMalformed(s string) string {
	if utf16Len(s) > 36 {
		return "UUID string too large"
	}
	parts := strings.Split(s, "-")
	if len(parts) != 5 {
		return "Invalid UUID string: " + s
	}
	for _, part := range parts {
		if part == "" {
			return "For input string: \"\" under radix 16"
		}
		digits := part
		if digits[0] == '+' {
			digits = digits[1:]
		}
		if digits == "" {
			return "Error at index 1 in: \"" + part + "\""
		}
		var total uint64
		for i, c := range digits {
			n, err := strconv.ParseUint(string(c), 16, 4)
			if err != nil || total > (1<<63-1-n)/16 {
				index := i
				if part[0] == '+' {
					index++
				}
				return "Error at index " + strconv.Itoa(index) + " in: \"" + part + "\""
			}
			total = total*16 + n
		}
	}
	return ""
}

// UntrustedURIMalformed returns the diagnostic of an open_url URI.
// vanilla: ExtraCodecs.UNTRUSTED_URI, Util.parseAndValidateUntrustedUri
func UntrustedURIMalformed(s string) string {
	fail := func(reason string, at int) string { return uriDiagnosticMessage(s, reason, at) }
	colon := strings.IndexAny(s, ":/?#")
	if colon >= 0 && s[colon] != ':' {
		colon = -1
	}
	if colon == 0 {
		return fail("Expected scheme name", 0)
	}
	if colon > 0 && !validURIScheme(s[:colon]) {
		at := 0
		if isURISchemeAlpha(s[0]) {
			for at < colon && (isURISchemeAlpha(s[at]) || s[at] >= '0' && s[at] <= '9' || strings.ContainsRune("+.-", rune(s[at]))) {
				at++
			}
		}
		return fail("Illegal character in scheme name", at)
	}
	start := 0
	if colon >= 0 {
		start = colon + 1
	}
	if colon >= 0 && (start == len(s) || s[start] == '#') {
		return fail("Expected scheme-specific part", start)
	}
	end := len(s)
	if i := strings.IndexByte(s[start:], '#'); i >= 0 {
		end = start + i
	}
	if colon < 0 || strings.HasPrefix(s[start:], "/") {
		if strings.HasPrefix(s[start:], "//") {
			start += 2
			authorityEnd := end
			if i := strings.IndexAny(s[start:end], "/?"); i >= 0 {
				authorityEnd = start + i
			}
			if authorityEnd == start && start == len(s) {
				return fail("Expected authority", start)
			}
			if msg := uriAuthorityMessage(s, start, authorityEnd); msg != "" {
				return msg
			}
			start = authorityEnd
		}
		pathEnd := end
		if i := strings.IndexByte(s[start:end], '?'); i >= 0 {
			pathEnd = start + i
		}
		if msg := uriComponentMessage(s, start, pathEnd, "path", false); msg != "" {
			return msg
		}
		if pathEnd < end {
			if msg := uriComponentMessage(s, pathEnd+1, end, "query", true); msg != "" {
				return msg
			}
		}
	} else if msg := uriComponentMessage(s, start, end, "opaque part", true); msg != "" {
		return msg
	}
	if end < len(s) {
		if msg := uriComponentMessage(s, end+1, len(s), "fragment", true); msg != "" {
			return msg
		}
	}
	if colon < 0 {
		return fail("Missing protocol in URI: "+s, -1)
	}
	if scheme := strings.ToLower(s[:colon]); scheme != "http" && scheme != "https" {
		return fail("Unsupported protocol in URI: "+s, -1)
	}
	return ""
}

func uriDiagnosticMessage(s, reason string, at int) string {
	if at >= 0 {
		return reason + " at index " + strconv.Itoa(utf16Len(s[:at])) + ": " + s
	}
	return reason + ": " + s
}

func uriComponentMessage(s string, start, end int, component string, brackets bool) string {
	for i := start; i < end; {
		c := s[i]
		if c == '%' {
			if i+2 >= end || !isHexDigit(s[i+1]) || !isHexDigit(s[i+2]) {
				return uriDiagnosticMessage(s, "Malformed escape pair", i)
			}
			i += 3
			continue
		}
		size := 1
		valid := isURIChar(c) && c != '#' || brackets && (c == '[' || c == ']')
		if c >= 0x80 {
			r, n := utf8.DecodeRuneInString(s[i:end])
			size = n
			valid = r > 128 && !unicode.IsSpace(r) && !unicode.IsControl(r)
		}
		if !valid {
			return uriDiagnosticMessage(s, "Illegal character in "+component, i)
		}
		i += size
	}
	return ""
}

// A non-server authority may fall back to a registry name; brackets force
// the server form and retain its hostname, IPv6 and port diagnostics.
// vanilla: java.net.URI.Parser.parseAuthority, parseServer
func uriAuthorityMessage(s string, start, end int) string {
	a := s[start:end]
	if !strings.ContainsAny(a, "[]") && validURIComponent(a, false) {
		return ""
	}
	fail := func(reason string, at int) string { return uriDiagnosticMessage(s, reason, at) }
	if !strings.ContainsAny(a, "[]") {
		return uriComponentMessage(s, start, end, "authority", false)
	}
	// Server characters allow literal percent signs in scoped IPv6 addresses.
	for i := start; i < end; i++ {
		if s[i] == '%' {
			if strings.Contains(a, "]") {
				continue
			}
			if i+2 >= end || !isHexDigit(s[i+1]) || !isHexDigit(s[i+2]) {
				return fail("Malformed escape pair", i)
			}
			i += 2
			continue
		}
		if s[i] == '[' || s[i] == ']' {
			continue
		}
		_, size := utf8.DecodeRuneInString(s[i:end])
		if msg := uriComponentMessage(s, i, i+size, "authority", false); msg != "" {
			return msg
		}
		i += size - 1
	}
	host := start
	if at := strings.IndexByte(a, '@'); at >= 0 {
		if msg := uriComponentMessage(s, start, start+at, "user info", false); msg != "" {
			return msg
		}
		host = start + at + 1
	}
	if host >= end || s[host] != '[' {
		for i := host; i < end; i++ {
			if !isURISchemeAlpha(s[i]) && (s[i] < '0' || s[i] > '9') && s[i] != '-' && s[i] != '.' {
				return fail("Illegal character in hostname", i)
			}
		}
		return fail("Expected hostname", host)
	}
	close := strings.IndexByte(s[host+1:end], ']')
	if close < 0 {
		return fail("Expected closing bracket for IPv6 address", end)
	}
	if close == 0 {
		return fail("Expected closing bracket for IPv6 address", host+1)
	}
	close += host + 1
	ipEnd := close
	if percent := strings.IndexByte(s[host+1:close], '%'); percent >= 0 {
		ipEnd = host + 1 + percent
	}
	if msg := uriIPv6Message(s, host+1, ipEnd); msg != "" {
		return msg
	}
	if ipEnd < close {
		if ipEnd+1 == close {
			return fail("scope id expected", -1)
		}
		for i := ipEnd + 1; i < close; i++ {
			c := s[i]
			if !isURISchemeAlpha(c) && (c < '0' || c > '9') && c != '.' && c != '_' {
				return fail("Illegal character in scope id", i)
			}
		}
	}
	port := close + 1
	if port < end && s[port] != ':' {
		return fail("Expected port number", port)
	}
	if port < end {
		port++
		for i := port; i < end; i++ {
			if s[i] < '0' || s[i] > '9' {
				return fail("Illegal character in port number", i)
			}
		}
		if port < end {
			if _, err := strconv.ParseInt(s[port:end], 10, 32); err != nil {
				return fail("Malformed port number", port)
			}
		}
	}
	return ""
}

// vanilla: java.net.URI.Parser.parseIPv6Reference
func uriIPv6Message(s string, start, end int) string {
	fail := func(reason string, at int) string { return uriDiagnosticMessage(s, reason, at) }
	pos, groups, compressed := start, 0, false
	if strings.HasPrefix(s[pos:end], "::") {
		pos += 2
		compressed = true
	}
	for pos < end {
		mark := pos
		for pos < end && isHexDigit(s[pos]) {
			pos++
		}
		if pos < end && s[pos] == '.' {
			// Embedded IPv4 consumes two groups.
			pos = mark
			for octet := range 4 {
				mark = pos
				for pos < end && s[pos] >= '0' && s[pos] <= '9' {
					pos++
				}
				n, err := strconv.ParseUint(strings.TrimLeft(s[mark:pos], "0"), 10, 8)
				if mark == pos || err != nil && n != 0 || n > 255 {
					return fail("Malformed IPv4 address", mark)
				}
				if octet < 3 {
					if pos == end || s[pos] != '.' {
						return fail("Malformed IPv4 address", pos)
					}
					pos++
				}
			}
			if pos != end {
				return fail("Malformed IPv4 address", pos)
			}
			groups += 2
			break
		}
		if pos == mark {
			reason := "Malformed IPv6 address"
			at := start
			if compressed {
				reason, at = "Expected hex digits or IPv4 address", pos
			} else if pos > start {
				reason, at = "Expected digits for an IPv6 address", pos
			}
			return fail(reason, at)
		}
		if pos-mark > 4 {
			return fail("IPv6 hexadecimal digit sequence too long", mark)
		}
		groups++
		if pos == end {
			break
		}
		if s[pos] != ':' {
			return fail("Malformed IPv6 address", start)
		}
		pos++
		if pos < end && s[pos] == ':' {
			if compressed {
				return fail("Malformed IPv6 address", start)
			}
			compressed = true
			pos++
		} else if pos == end {
			return fail("Expected digits for an IPv6 address", pos)
		}
	}
	if groups > 8 {
		return fail("IPv6 address too long", start)
	}
	if !compressed && groups < 8 {
		return fail("IPv6 address too short", start)
	}
	if compressed && groups == 8 {
		return fail("Malformed IPv6 address", start)
	}
	return ""
}
