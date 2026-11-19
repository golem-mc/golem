package command

import (
	"math"
	"math/big"
	mathbits "math/bits"
	"slices"
)

// The binary-to-decimal conversion java.text.DigitList reads its digits
// from, jdk.internal.math.FloatingDecimal.BinaryToASCIIBuffer.dtoa. Java 19
// took this converter out of Double.toString but DecimalFormat still runs
// it, so it is the digits a macro's float or double substitutes as: it can
// develop one or two more digits than round-tripping needs (roundedUp
// tells the fifteen-digit rounding below to not double-round) and it stops
// its division bracketed by half the value's neighborhood. The int and long
// paths' wrapping arithmetic is Java's, byte for byte.

// binaryToDecimalDigits develops the digits of a finite positive double,
// answering the digit string (which may end in zeros), the decimal exponent
// such that the value is 0.digits × 10^decExp, and whether the conversion
// rounded its last digit up or landed on the value's own exact digits.
func binaryToDecimalDigits(v float64) (digits string, decExp int, roundedUp, exact bool) {
	rawBits := math.Float64bits(v)
	fractBits := rawBits & (1<<52 - 1)
	binExp := int(rawBits>>52) & 0x7ff
	nSignificantBits := 53
	if binExp == 0 {
		// A denormalized mantissa renormalizes by its leading zeros.
		leadingZeros := mathbits.LeadingZeros64(fractBits)
		shift := leadingZeros - 11
		fractBits <<= shift
		binExp = 1 - shift
		nSignificantBits = 64 - leadingZeros
	} else {
		fractBits |= 1 << 52
	}
	binExp -= 1023
	tailZeros := mathbits.TrailingZeros64(fractBits)
	nFractBits := 53 - tailZeros
	nTinyBits := max(nFractBits-binExp-1, 0)
	// The easy case: a whole number, so every interesting bit sits left of
	// the point and the integer-scaled mantissa fits a long.
	if binExp <= 62 && binExp >= -21 && nTinyBits == 0 {
		insignificant := 0
		if binExp > nSignificantBits {
			insignificant = fdInsignificantDigitsForPow2(binExp - nSignificantBits - 1)
		}
		f := int64(fractBits)
		if binExp >= 52 {
			f <<= binExp - 52
		} else {
			f >>= 52 - binExp
		}
		return developLongDigits(0, f, insignificant)
	}
	// The hard case: divide B by S for the digits and stop bracketed by M,
	// half the value's neighborhood.
	decExp = fdEstimateDecExp(int64(fractBits), binExp)
	b5 := max(-decExp, 0)
	b2 := b5 + nTinyBits + binExp
	s5 := max(decExp, 0)
	s2 := s5 + nTinyBits
	m5 := b5
	m2 := b2 - nSignificantBits
	f := int64(fractBits) >> tailZeros
	b2 -= nFractBits - 1
	common2 := min(b2, s2)
	b2, s2, m2 = b2-common2, s2-common2, m2-common2
	if nFractBits == 1 {
		// Below a pure power of two the next double is half as far as
		// the symmetric half suggests.
		m2--
	}
	if m2 < 0 {
		b2, s2, m2 = b2-m2, s2-m2, 0
	}
	// Java's size estimates pick the arithmetic the bracket runs in.
	if bbits, tenSbits := nFractBits+b2+fd5Bits(b5), s2+1+fd5Bits(s5+1); bbits < 32 && tenSbits < 32 {
		return developSmallDigits(int32(f), fdSmall5Pow[b5], fdSmall5Pow[s5], fdSmall5Pow[m5], b2, s2, m2, decExp)
	} else if bbits < 64 && tenSbits < 64 {
		return developSmallDigits(f, fdLong5Pow[b5], fdLong5Pow[s5], fdLong5Pow[m5], b2, s2, m2, decExp)
	}
	return developBigDigits(f, b5, b2, s5, s2, m5, m2, decExp)
}

// developLongDigits writes the integer the easy case hands it, dropping
// insignificant low-order digits after rounding the residue up at half,
// then stripping trailing zeros into the decimal exponent. The answer
// keeps the 0.digits × 10^decExp reading.
func developLongDigits(decExp int, lvalue int64, insignificant int) (string, int, bool, bool) {
	if insignificant != 0 {
		pow10 := fdLong5Pow[insignificant] << insignificant // 5^i << i == 10^i
		residue := lvalue % pow10
		lvalue /= pow10
		decExp += insignificant
		if residue >= pow10>>1 {
			lvalue++
		}
	}
	for lvalue%10 == 0 {
		decExp++
		lvalue /= 10
	}
	digits := make([]byte, 0, 20)
	for ; lvalue != 0; lvalue /= 10 {
		digits = append(digits, byte('0'+lvalue%10))
	}
	slices.Reverse(digits)
	return string(digits), decExp + len(digits), false, false
}

// developSmallDigits runs the division bracket on fixed-width arithmetic;
// the wrap where the small width overflows mid-correction is Java's own.
// The bracket's high side is open here: b+m has to beat a full digit.
func developSmallDigits[T int32 | int64](f, bPow5, sPow5, mPow5 T, b2, s2, m2, decExp int) (digits string, decExpOut int, roundedUp, exact bool) {
	b := f * bPow5 << b2
	s := sPow5 << s2
	m := mPow5 << m2
	tens := s * 10
	d := make([]byte, 0, 24)
	// The first iteration is unrolled: a too-high decExp estimate shows as
	// a zero quotient and folds into the estimate instead of the digits.
	q := b / s
	b = 10 * (b % s)
	m *= 10
	low := b < m
	high := b+m > tens
	if q != 0 || high {
		d = append(d, byte('0'+q))
	} else {
		decExp--
	}
	// Exponential form promises a digit after the point, so the bracket
	// cannot stop the conversion at one digit there.
	if decExp < -3 || decExp >= 8 {
		low, high = false, false
	}
	for !low && !high {
		q = b / s
		b = 10 * (b % s)
		m *= 10
		if m > 0 {
			low = b < m
			high = b+m > tens
		} else {
			low, high = true, true
		}
		d = append(d, byte('0'+q))
	}
	exact = b == 0
	decExpOut, roundedUp = roundLastDevelopedDigit(d, decExp+1, high, low, int(b<<1-tens))
	return string(d), decExpOut, roundedUp, exact
}

// developBigDigits is the bracket for the values the fixed widths cannot
// hold. Its high side is closed (b+m reaching a full digit suffices) where
// the small path's was open — the two disagree exactly there, and Java's
// estimates decide which one a value runs.
func developBigDigits(f int64, b5, b2, s5, s2, m5, m2, decExp int) (digits string, decExpOut int, roundedUp, exact bool) {
	b := fdMulPow52(f, b5, b2)
	s := fdMulPow52(1, s5, s2)
	m := fdMulPow52(1, m5, m2)
	tens := fdMulPow52(1, s5+1, s2+1)
	dto := func() *big.Int {
		q, r := new(big.Int).QuoRem(b, s, new(big.Int))
		b.Mul(r, big.NewInt(10))
		return q
	}
	d := make([]byte, 0, 24)
	// The half is already scaled by ten for this unrolled round, as the
	// small path's m *= 10 did.
	m.Mul(m, big.NewInt(10))
	q := dto()
	low := b.Cmp(m) < 0
	high := new(big.Int).Add(b, m).Cmp(tens) >= 0
	if q.Sign() != 0 || high {
		d = append(d, byte('0'+q.Int64()))
	} else {
		decExp--
	}
	if decExp < -3 || decExp >= 8 {
		low, high = false, false
	}
	sum := new(big.Int)
	for !low && !high {
		q = dto()
		m.Mul(m, big.NewInt(10))
		low = b.Cmp(m) < 0
		high = sum.Add(b, m).Cmp(tens) >= 0
		d = append(d, byte('0'+q.Int64()))
	}
	lowDigitDifference := 0
	if high && low {
		// An exact tie shows only when both brackets closed.
		lowDigitDifference = b.Lsh(b, 1).Cmp(tens)
	}
	exact = b.Sign() == 0
	decExpOut, roundedUp = roundLastDevelopedDigit(d, decExp+1, high, low, lowDigitDifference)
	return string(d), decExpOut, roundedUp, exact
}

// roundLastDevelopedDigit rounds the bracket's last digit the way it
// stopped: up unless the value sat below the half, and at an exact tie
// half to even on that digit. It answers the adjusted decimal exponent
// and whether it rounded.
func roundLastDevelopedDigit(d []byte, decExp int, high, low bool, lowDigitDifference int) (int, bool) {
	round := high && (!low || lowDigitDifference > 0)
	if high && low && lowDigitDifference == 0 {
		// A tie: fall to the digit's own parity.
		round = d[len(d)-1]&1 != 0
	}
	if !round {
		return decExp, false
	}
	i := len(d) - 1
	for i >= 0 && d[i] == '9' {
		d[i] = '0'
		i--
	}
	if i < 0 {
		// The carry ran the whole number over.
		d[0] = '1'
		return decExp + 1, false
	}
	d[i]++
	return decExp, true
}

// fdEstimateDecExp estimates floor(log10 v) with the same float arithmetic
// and bit surgery Java does, so the digit development below starts at the
// same decimal digit.
func fdEstimateDecExp(fractBits int64, binExp int) int {
	d2 := math.Float64frombits(1023<<52 | uint64(fractBits)&(1<<52-1))
	d := (d2-1.5)*0.289529654 + 0.176091259 + float64(binExp)*0.301029995663981
	dBits := math.Float64bits(d)
	exponent := int(dBits>>52)&0x7ff - 1023
	isNegative := dBits&(1<<63) != 0
	switch {
	case exponent >= 0 && exponent < 52:
		r := int((dBits&(1<<52-1) | 1<<52) >> (52 - exponent))
		if !isNegative {
			return r
		}
		if dBits&(1<<52-1)>>exponent == 0 {
			return -r
		}
		return -r - 1
	case exponent < 0:
		if dBits&(1<<63-1) == 0 {
			return 0
		}
		if isNegative {
			return -1
		}
		return 0
	}
	return int(d)
}

func fdInsignificantDigitsForPow2(p2 int) int {
	if p2 > 1 && p2 < len(fdInsignificantDigits) {
		return fdInsignificantDigits[p2]
	}
	return 0
}

// fdMulPow52 answers value × 5^p5 × 2^p2.
func fdMulPow52(value int64, p5, p2 int) *big.Int {
	v := big.NewInt(value)
	v.Mul(v, new(big.Int).Exp(big.NewInt(5), big.NewInt(int64(p5)), nil))
	return v.Lsh(v, uint(p2))
}

// fdN5Bits is the bit length of 5^i; past the table Java grows by three a
// digit.
var fdN5Bits = [...]int{
	0, 3, 5, 7, 10, 12, 14, 17, 19, 21, 24, 26, 28, 31,
	33, 35, 38, 40, 42, 45, 47, 49, 52, 54, 56, 59, 61,
}

func fd5Bits(i int) int {
	if i < len(fdN5Bits) {
		return fdN5Bits[i]
	}
	return i * 3
}

var fdSmall5Pow = [...]int32{
	1, 5, 25, 125, 625, 3125, 15625, 78125,
	390625, 1953125, 9765625, 48828125, 244140625, 1220703125,
}

var fdLong5Pow = [...]int64{
	1, 5, 25, 125, 625, 3125, 15625, 78125,
	390625, 1953125, 9765625, 48828125, 244140625, 1220703125,
	6103515625, 30517578125, 152587890625, 762939453125, 3814697265625,
	19073486328125, 95367431640625, 476837158203125, 2384185791015625,
	11920928955078125, 59604644775390625, 298023223876953125, 1490116119384765625,
}

// fdInsignificantDigits[p2] is how many decimal digits of 2^p2 sit below a
// long's worth of significance.
var fdInsignificantDigits = [...]int{
	0, 0, 0, 0, 1, 1, 1, 2, 2, 2, 3, 3, 3, 3,
	4, 4, 4, 5, 5, 5, 6, 6, 6, 6, 7, 7, 7,
	8, 8, 8, 9, 9, 9, 9, 10, 10, 10, 11, 11, 11,
	12, 12, 12, 12, 13, 13, 13, 14, 14, 14,
	15, 15, 15, 15, 16, 16, 16, 17, 17, 17,
	18, 18, 18, 19,
}
