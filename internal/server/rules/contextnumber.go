package rules

import (
	"errors"
	"fmt"
	"math"

	"github.com/golem-mc/golem/internal/server/world"
	"github.com/golem-mc/golem/nbt"
	"github.com/golem-mc/golem/registry"
)

// ArithmeticError is the ArithmeticException a context provider throws, its
// message.
type ArithmeticError string

func (e ArithmeticError) Error() string { return string(e) }

const (
	ErrIntegerOverflow = ArithmeticError("integer overflow")
	ErrDivideByZero    = ArithmeticError("/ by zero")
)

// ErrLookupIndex is the IndexOutOfBoundsException a lookup level-based value
// throws below level 1. Neither getInt nor ComputeCommand catches it, so a
// command answers command.failed.
// vanilla: LevelBasedValue.Lookup.calculate
var ErrLookupIndex = errors.New("lookup level-based value below level 1")

// maxProviderRefDepth bounds how many registry entries one evaluation may
// pass through, so that entries referring to each other end in an error
// instead of a stack overflow.
const maxProviderRefDepth = 64

// EvalIntProvider is a context int provider's value, or the error vanilla's
// getIntUnsafe throws.
func EvalIntProvider(n registry.LootNumber, ctx *LootContext, rng LootRand) (int32, error) {
	r := lootRoller{ctx: ctx, rng: rng}
	return r.intOf(n)
}

// EvalFloatProvider is a context float provider's value, which may be
// non-finite, or the error vanilla's getFloatUnsafe throws.
func EvalFloatProvider(n registry.LootNumber, ctx *LootContext, rng LootRand) (float32, error) {
	r := lootRoller{ctx: ctx, rng: rng}
	v, err := r.floatOf(n)
	return float32(v), err
}

// numInt reads a number provider as an integer; a provider that throws is 0.
// vanilla: ContextIntProvider.getInt
func (r *lootRoller) numInt(n registry.LootNumber) int32 {
	v, err := r.intOf(n)
	if err != nil {
		return 0
	}
	return v
}

// numFloat reads a number provider as a float; a provider that throws or is
// not finite is 0.
// vanilla: ContextFloatProvider.getFloat
func (r *lootRoller) numFloat(n registry.LootNumber) float64 {
	v, err := r.floatOf(n)
	if err != nil || math.IsNaN(v) || math.IsInf(v, 0) {
		return 0
	}
	return v
}

// intOf evaluates a provider held in an int field: a constant rounded, a
// uniform rolled between its bounds with both ends in, a binomial counted
// out one coin a round. A nil provider is zero.
// vanilla: ContextIntProvider.getIntUnsafe
func (r *lootRoller) intOf(n registry.LootNumber) (int32, error) {
	switch k := n.(type) {
	case nil:
		return 0, nil
	case registry.LootConstant:
		return roundInt(k.Value), nil
	case registry.LootUniform:
		low, err := r.intOf(k.Min)
		if err != nil {
			return 0, err
		}
		high, err := r.intOf(k.Max)
		if err != nil {
			return 0, err
		}
		if low >= high {
			return low, nil
		}
		return low + r.rng.Int31n(high-low+1), nil
	case registry.LootBinomial:
		n, err := r.intOf(k.N)
		if err != nil {
			return 0, err
		}
		p, err := r.floatOf(k.P)
		if err != nil {
			return 0, err
		}
		if math.IsNaN(p) || math.IsInf(p, 0) {
			return 0, ArithmeticError("Invalid value: " + javaFloat(p))
		}
		count := int32(0)
		for range n {
			if float64(r.rng.Float32()) < float64(float32(p)) {
				count++
			}
		}
		return count, nil
	case registry.LootSum:
		vals, err := r.intsOf(k.Summands)
		if err != nil {
			return 0, err
		}
		var s int64
		for _, v := range vals {
			s += int64(v)
		}
		return narrowInt(s)
	case registry.LootUnary:
		v, err := r.intOf(k.Input)
		if err != nil {
			return 0, err
		}
		switch k.Op {
		case "abs":
			if v == math.MinInt32 {
				return 0, ArithmeticError("Overflow to represent absolute value of Integer.MIN_VALUE")
			}
			return max(v, -v), nil
		case "negate":
			return exactResult(-int64(v))
		}
	case registry.LootBinary:
		a, err := r.intOf(k.Left)
		if err != nil {
			return 0, err
		}
		b, err := r.intOf(k.Right)
		if err != nil {
			return 0, err
		}
		return intBinary(k.Op, a, b)
	case registry.LootAggregate:
		vals, err := r.intsOf(k.Inputs)
		if err != nil {
			return 0, err
		}
		return intAggregate(k.Op, vals)
	case registry.LootConvert:
		v, err := r.floatOf(k.Input)
		if err != nil {
			return 0, err
		}
		f := float32(v)
		if math.IsNaN(v) || math.IsInf(float64(f), 0) {
			return 0, ArithmeticError("Value " + javaFloat(v) + " can't be safely converted to int")
		}
		return narrowInt(world.JavaLong(float64(f)))
	case registry.LootWeightedList:
		return r.intOf(k.Values[pickWeighted(r.rng, k.Weights)])
	case registry.LootConditional:
		if r.condition(k.Condition) {
			return r.intOf(k.OnTrue)
		}
		return r.intOf(k.OnFalse)
	case registry.LootDispatcher:
		for _, c := range k.Cases {
			if r.condition(c.Condition) {
				return r.intOf(c.Value)
			}
		}
		return r.intOf(k.Default)
	case registry.LootScore:
		if score, ok := r.score(k); ok {
			return score, nil
		}
		return r.intOf(k.Fallback)
	case registry.LootStorage:
		if v, ok := nbt.IntValue(r.stored(k)); ok {
			return v, nil
		}
		return r.intOf(k.Fallback)
	case registry.LootProviderRef:
		var v int32
		err := r.ref(k, func(n registry.LootNumber) (err error) {
			v, err = r.intOf(n)
			return err
		})
		return v, err
	}
	return 0, nil
}

// floatOf evaluates a provider held in a float field, in float32 arithmetic
// widened to float64 (the generated tables' constants and uniforms are the
// exception, read as written). A nil provider is zero.
// vanilla: ContextFloatProvider.getFloatUnsafe
func (r *lootRoller) floatOf(n registry.LootNumber) (float64, error) {
	switch k := n.(type) {
	case nil:
		return 0, nil
	case registry.LootConstant:
		return k.Value, nil
	case registry.LootUniform:
		low, err := r.floatOf(k.Min)
		if err != nil {
			return 0, err
		}
		high, err := r.floatOf(k.Max)
		if err != nil {
			return 0, err
		}
		if low >= high {
			return low, nil
		}
		// vanilla: Mth.nextFloat, float32 arithmetic
		return float64(r.rng.Float32()*(float32(high)-float32(low)) + float32(low)), nil
	case registry.LootBinomial:
		v, err := r.intOf(k)
		return float64(v), err
	case registry.LootSum:
		vals, err := r.floatsOf(k.Summands)
		var s float32
		for _, v := range vals {
			s += v
		}
		return float64(s), err
	case registry.LootUnary:
		v, err := r.floatOf(k.Input)
		if err != nil {
			return 0, err
		}
		return float64(floatUnary(k.Op, float32(v))), nil
	case registry.LootBinary:
		a, err := r.floatOf(k.Left)
		if err != nil {
			return 0, err
		}
		b, err := r.floatOf(k.Right)
		if err != nil {
			return 0, err
		}
		return float64(floatBinary(k.Op, float32(a), float32(b))), nil
	case registry.LootAggregate:
		vals, err := r.floatsOf(k.Inputs)
		if err != nil {
			return 0, err
		}
		return float64(floatAggregate(k.Op, vals)), nil
	case registry.LootConvert:
		v, err := r.intOf(k.Input)
		return float64(float32(v)), err
	case registry.LootWeightedList:
		return r.floatOf(k.Values[pickWeighted(r.rng, k.Weights)])
	case registry.LootConditional:
		if r.condition(k.Condition) {
			return r.floatOf(k.OnTrue)
		}
		return r.floatOf(k.OnFalse)
	case registry.LootDispatcher:
		for _, c := range k.Cases {
			if r.condition(c.Condition) {
				return r.floatOf(c.Value)
			}
		}
		return r.floatOf(k.Default)
	case registry.LootScore:
		if score, ok := r.score(k); ok {
			return float64(float32(score)), nil
		}
		return r.floatOf(k.Fallback)
	case registry.LootStorage:
		if v, ok := nbt.NumberValue(r.stored(k)); ok {
			return float64(float32(v)), nil
		}
		return r.floatOf(k.Fallback)
	case registry.LootEnvAttribute:
		if r.ctx.Level == nil {
			return 0, nil
		}
		return float64(r.ctx.Level.EnvironmentFloat(k.Attribute)), nil
	case registry.LootEnchantmentLevel:
		if enchLevelValueThrows(k.Amount, r.ctx.EnchantmentLevel) {
			return 0, ErrLookupIndex
		}
		return float64(EnchLevelValueAt(k.Amount, r.ctx.EnchantmentLevel)), nil
	case registry.LootProviderRef:
		var v float64
		err := r.ref(k, func(n registry.LootNumber) (err error) {
			v, err = r.floatOf(n)
			return err
		})
		return v, err
	}
	return 0, nil
}

func (r *lootRoller) intsOf(ns []registry.LootNumber) ([]int32, error) {
	vals := make([]int32, len(ns))
	for i, n := range ns {
		v, err := r.intOf(n)
		if err != nil {
			return nil, err
		}
		vals[i] = v
	}
	return vals, nil
}

func (r *lootRoller) floatsOf(ns []registry.LootNumber) ([]float32, error) {
	vals := make([]float32, len(ns))
	for i, n := range ns {
		v, err := r.floatOf(n)
		if err != nil {
			return nil, err
		}
		vals[i] = float32(v)
	}
	return vals, nil
}

// ref evaluates the registry entry a holder names one entry deeper.
// Resolving holders here keeps cyclic references within the depth limit.
func (r *lootRoller) ref(k registry.LootProviderRef, eval func(registry.LootNumber) error) error {
	if r.refDepth >= maxProviderRefDepth {
		return ArithmeticError("context provider " + k.Name + " refers to itself")
	}
	n, msg := registry.ResolveContextProvider(k)
	if msg != "" {
		return ArithmeticError(msg)
	}
	r.refDepth++
	defer func() { r.refDepth-- }()
	return eval(n)
}

// score reads a score provider's holder's score; false when the holder, the
// level or the score is absent.
// vanilla: ScoreboardValue
func (r *lootRoller) score(k registry.LootScore) (int32, bool) {
	if r.ctx.Level == nil {
		return 0, false
	}
	holder := k.Target
	if !k.Fixed {
		holder = r.entity(k.Target).ScoreHolder
		if holder == "" {
			return 0, false
		}
	}
	return r.ctx.Level.Score(holder, k.Objective)
}

// stored is the single number a storage provider's path selects, nil when
// there is none.
// vanilla: StoredNumberAccess
func (r *lootRoller) stored(k registry.LootStorage) nbt.Tag {
	if r.ctx.Level == nil {
		return nil
	}
	t, _ := r.ctx.Level.StorageNumber(k.Storage, k.Path)
	return t
}

func intBinary(op string, a, b int32) (int32, error) {
	switch op {
	case "sub":
		return exactResult(int64(a) - int64(b))
	case "mod":
		if b == 0 {
			return 0, ErrDivideByZero
		}
		if b == -1 {
			return 0, nil
		}
		return a % b, nil
	case "div":
		if b == 0 {
			return 0, ErrDivideByZero
		}
		if b == -1 {
			return -a, nil
		}
		return a / b, nil
	case "floor_div":
		return floorDiv(a, b)
	case "floor_mod":
		return floorMod(a, b)
	case "pow":
		return powExact(a, b)
	}
	return 0, nil
}

func intAggregate(op string, vals []int32) (int32, error) {
	switch op {
	case "mul":
		p := int64(1)
		for _, v := range vals {
			p *= int64(v)
		}
		return narrowInt(p)
	case "max":
		m := int32(math.MinInt32)
		for _, v := range vals {
			m = max(m, v)
		}
		return m, nil
	case "min":
		m := int32(math.MaxInt32)
		for _, v := range vals {
			m = min(m, v)
		}
		return m, nil
	case "avg":
		if len(vals) == 0 {
			return 0, ErrDivideByZero
		}
		var s int64
		for _, v := range vals {
			s += int64(v)
		}
		return narrowInt(s / int64(len(vals)))
	}
	return 0, nil
}

func floatUnary(op string, v float32) float32 {
	switch op {
	case "abs":
		return float32(math.Abs(float64(v)))
	case "negate":
		return -v
	case "ceil":
		return mthCeil(v)
	case "floor":
		return float32(math.Floor(float64(v)))
	case "round":
		return javaRound(v)
	case "truncate":
		if v > 0 {
			return float32(math.Floor(float64(v)))
		}
		return float32(math.Ceil(float64(v)))
	case "sqrt":
		return float32(math.Sqrt(float64(v)))
	case "sin":
		return world.MthSin(float64(v))
	case "cos":
		return world.MthCos(float64(v))
	}
	return 0
}

func floatBinary(op string, a, b float32) float32 {
	switch op {
	case "sub":
		return a - b
	case "div":
		return a / b
	case "mod":
		if b == 0 {
			return float32(math.NaN())
		}
		return float32(math.Mod(float64(a), float64(b)))
	case "pow":
		if a == 0 && b == 0 {
			return float32(math.NaN())
		}
		return float32(math.Pow(float64(a), float64(b)))
	}
	return 0
}

func floatAggregate(op string, vals []float32) float32 {
	switch op {
	case "mul":
		p := float32(1)
		for _, v := range vals {
			p *= v
		}
		return p
	case "max":
		m := float32(-math.MaxFloat32)
		for _, v := range vals {
			m = float32(math.Max(float64(m), float64(v)))
		}
		return m
	case "min":
		m := float32(math.MaxFloat32)
		for _, v := range vals {
			m = float32(math.Min(float64(m), float64(v)))
		}
		return m
	case "avg":
		var s float32
		for _, v := range vals {
			s += v
		}
		return s / float32(len(vals))
	case "length":
		var s float32
		for _, v := range vals {
			s += v * v
		}
		return float32(math.Sqrt(float64(s)))
	}
	return 0
}

// javaFloat renders a float32 value as Float.toString does.
func javaFloat(v float64) string { return nbt.JavaFloatText(float64(float32(v)), 32) }

// JavaF2I is Java's (int) cast of a floating value: truncation, saturating,
// NaN to zero.
func JavaF2I(v float64) int32 {
	switch {
	case math.IsNaN(v):
		return 0
	case v >= math.MaxInt32:
		return math.MaxInt32
	case v <= math.MinInt32:
		return math.MinInt32
	}
	return int32(v)
}

// narrowInt is ContextIntProvider.longToIntSafe.
func narrowInt(v int64) (int32, error) {
	if int64(int32(v)) != v {
		return 0, ArithmeticError(fmt.Sprintf("Value %d can't be safely converted to int", v))
	}
	return int32(v), nil
}

func exactResult(v int64) (int32, error) {
	if int64(int32(v)) != v {
		return 0, ErrIntegerOverflow
	}
	return int32(v), nil
}

func floorDiv(a, b int32) (int32, error) {
	if b == 0 {
		return 0, ErrDivideByZero
	}
	if a == math.MinInt32 && b == -1 {
		return 0, ErrIntegerOverflow
	}
	q := a / b
	if (a%b != 0) && ((a < 0) != (b < 0)) {
		q--
	}
	return q, nil
}

func floorMod(a, b int32) (int32, error) {
	if b == 0 {
		return 0, ErrDivideByZero
	}
	if b == -1 {
		return 0, nil
	}
	m := a % b
	if m != 0 && (m < 0) != (b < 0) {
		m += b
	}
	return m, nil
}

// powExact is Math.powExact(int, int).
func powExact(base, exp int32) (int32, error) {
	if base == 0 && exp == 0 {
		return 0, ArithmeticError("Result of 0 to the power of 0 is undefined")
	}
	if exp < 0 {
		return 0, ArithmeticError("Negative exponent")
	}
	result := int64(1)
	for range exp {
		result *= int64(base)
		if int64(int32(result)) != result {
			return 0, ErrIntegerOverflow
		}
		if result == 0 || result == 1 && base == 1 {
			break
		}
	}
	return int32(result), nil
}

// javaRound is Math.round(float) widened back to a float.
func javaRound(v float32) float32 {
	if v != v {
		return 0
	}
	return float32(JavaF2I(math.Floor(float64(v) + 0.5)))
}

// mthCeil is Mth.ceil(float), whose overflow wraps.
func mthCeil(v float32) float32 {
	i := JavaF2I(float64(v))
	if v > float32(i) {
		i++
	}
	return float32(i)
}

func pickWeighted(rng LootRand, weights []int64) int {
	var total int64
	for _, w := range weights {
		total += w
	}
	n := int64(rng.Int31n(int32(total)))
	for i, w := range weights {
		if n < w {
			return i
		}
		n -= w
	}
	return len(weights) - 1
}
