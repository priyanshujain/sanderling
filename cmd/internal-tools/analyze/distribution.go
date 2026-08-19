package main

import "math"

// standardNormalUpperTail is P(Z > z) for a standard normal Z.
func standardNormalUpperTail(z float64) float64 {
	return 0.5 * math.Erfc(z/math.Sqrt2)
}

// chiSquareUpperTail is P(X > x) for a chi-square variate with the given
// degrees of freedom, which is the regularized upper incomplete gamma
// Q(degreesOfFreedom/2, x/2).
func chiSquareUpperTail(x float64, degreesOfFreedom int) float64 {
	if degreesOfFreedom <= 0 || math.IsNaN(x) {
		return math.NaN()
	}
	if x <= 0 {
		return 1
	}
	return regularizedUpperGamma(float64(degreesOfFreedom)/2, x/2)
}

const (
	gammaIterationLimit = 2000
	gammaTolerance      = 1e-15
	gammaTiny           = 1e-300
)

// regularizedUpperGamma is Q(shape, x). The series is used below the crossover
// and the continued fraction above it, as in Numerical Recipes in C, 2nd ed.,
// section 6.2 (gammp/gammq).
func regularizedUpperGamma(shape, x float64) float64 {
	if x < shape+1 {
		return 1 - lowerGammaSeries(shape, x)
	}
	return upperGammaContinuedFraction(shape, x)
}

func lowerGammaSeries(shape, x float64) float64 {
	term := 1 / shape
	sum := term
	for iteration := 1; iteration < gammaIterationLimit; iteration++ {
		term *= x / (shape + float64(iteration))
		sum += term
		if math.Abs(term) < math.Abs(sum)*gammaTolerance {
			break
		}
	}
	return sum * math.Exp(-x+shape*math.Log(x)-logGamma(shape))
}

// upperGammaContinuedFraction evaluates Q(shape, x) with the modified Lentz
// algorithm, Numerical Recipes in C, 2nd ed., section 5.2.
func upperGammaContinuedFraction(shape, x float64) float64 {
	b := x + 1 - shape
	c := 1 / gammaTiny
	d := 1 / b
	h := d
	for iteration := 1; iteration < gammaIterationLimit; iteration++ {
		numerator := -float64(iteration) * (float64(iteration) - shape)
		b += 2
		d = numerator*d + b
		if math.Abs(d) < gammaTiny {
			d = gammaTiny
		}
		c = b + numerator/c
		if math.Abs(c) < gammaTiny {
			c = gammaTiny
		}
		d = 1 / d
		delta := d * c
		h *= delta
		if math.Abs(delta-1) < gammaTolerance {
			break
		}
	}
	return h * math.Exp(-x+shape*math.Log(x)-logGamma(shape))
}

func logGamma(x float64) float64 {
	value, _ := math.Lgamma(x)
	return value
}
