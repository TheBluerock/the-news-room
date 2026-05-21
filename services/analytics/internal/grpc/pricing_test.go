package grpcserver

import (
	"math"
	"testing"
)

func TestComputeCostUSD_KnownModels(t *testing.T) {
	cases := []struct {
		model    string
		prompt   int32
		complete int32
		want     float64
	}{
		// gpt-4o: $2.50/1M input, $10.00/1M output
		// 1000 in + 500 out = 0.0025 + 0.005 = 0.0075
		{"gpt-4o", 1000, 500, 0.0075},
		// claude: $3.00/1M input, $15.00/1M output
		{"claude-sonnet-4-6", 1000, 500, 0.003 + 0.0075},
		// zero tokens → zero cost
		{"gpt-4o", 0, 0, 0.0},
	}
	for _, c := range cases {
		got := computeCostUSD(c.model, c.prompt, c.complete)
		if math.Abs(got-c.want) > 1e-9 {
			t.Errorf("computeCostUSD(%q, %d, %d) = %v, want %v", c.model, c.prompt, c.complete, got, c.want)
		}
	}
}

func TestComputeCostUSD_UnknownModelReturnsZero(t *testing.T) {
	got := computeCostUSD("imaginary-model-v99", 5000, 2000)
	if got != 0.0 {
		t.Errorf("unknown model should yield 0 cost, got %v", got)
	}
}

func TestPricePerMToken_HasExpectedModels(t *testing.T) {
	for _, m := range []string{"gpt-4o", "claude-sonnet-4-6"} {
		if _, ok := PricePerMToken[m]; !ok {
			t.Errorf("PricePerMToken missing required entry for %q", m)
		}
	}
}

func TestPricePerMToken_RatesAreNonNegative(t *testing.T) {
	for model, rates := range PricePerMToken {
		if rates[0] < 0 || rates[1] < 0 {
			t.Errorf("PricePerMToken[%q] has negative rate: %v", model, rates)
		}
	}
}
