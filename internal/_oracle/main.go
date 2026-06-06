//go:build ignore

// Oracle generator for the bit-exact PCG port.
//
// Emits the golden fixture consumed by pkg/spec/test/pcg.test.ts. The fixture
// pins the exact draw sequences produced by Go's math/rand/v2 over a PCG source
// so the TypeScript port (pkg/spec/src/pcg.ts) can be asserted bit-for-bit.
//
// Run from the spec package to regenerate:
//
//	go run ./internal/_oracle > pkg/spec/test/fixtures/pcg-golden.json
package main

import (
	"encoding/json"
	"math/rand/v2"
	"os"
)

// seed carries the PCG seed pair. The values are emitted as decimal strings so
// JavaScript parses them into BigInt without the float precision loss it would
// suffer on values above 2^53.
type seed struct {
	Hi string `json:"hi"`
	Lo string `json:"lo"`
}

type caseEntry struct {
	Seed    seed             `json:"seed"`
	Uint64  []string         `json:"uint64"`
	Float64 []float64        `json:"float64"`
	IntN    map[string][]int `json:"intN"`
}

const drawCount = 40

var seeds = [][2]uint64{
	{1718000000000000000, 0},
	{42, 0},
	{0, 0},
	{1, 2},
	{18446744073709551615, 18446744073709551615},
}

var intNValues = []int{2, 3, 7, 15, 1000}

func main() {
	cases := make([]caseEntry, 0, len(seeds))

	for _, pair := range seeds {
		hi, lo := pair[0], pair[1]
		entry := caseEntry{
			Seed: seed{Hi: formatUint64(hi), Lo: formatUint64(lo)},
			IntN: map[string][]int{},
		}

		r := rand.New(rand.NewPCG(hi, lo))
		for i := 0; i < drawCount; i++ {
			entry.Uint64 = append(entry.Uint64, formatUint64(r.Uint64()))
		}

		r = rand.New(rand.NewPCG(hi, lo))
		for i := 0; i < drawCount; i++ {
			entry.Float64 = append(entry.Float64, r.Float64())
		}

		for _, n := range intNValues {
			r = rand.New(rand.NewPCG(hi, lo))
			draws := make([]int, 0, drawCount)
			for i := 0; i < drawCount; i++ {
				draws = append(draws, r.IntN(n))
			}
			entry.IntN[itoa(n)] = draws
		}

		cases = append(cases, entry)
	}

	encoder := json.NewEncoder(os.Stdout)
	encoder.SetIndent("", "  ")
	if err := encoder.Encode(cases); err != nil {
		panic(err)
	}
}

// formatUint64 renders a uint64 as a decimal string so JavaScript can parse it
// into a BigInt without precision loss.
func formatUint64(v uint64) string {
	if v == 0 {
		return "0"
	}
	var buf [20]byte
	i := len(buf)
	for v > 0 {
		i--
		buf[i] = byte('0' + v%10)
		v /= 10
	}
	return string(buf[i:])
}

func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	neg := n < 0
	if neg {
		n = -n
	}
	var buf [20]byte
	i := len(buf)
	for n > 0 {
		i--
		buf[i] = byte('0' + n%10)
		n /= 10
	}
	if neg {
		i--
		buf[i] = '-'
	}
	return string(buf[i:])
}
