package grpcserver

import "log"

// PricePerMToken is the static USD-per-1M-tokens table used to compute
// per-article LLM cost. Update when provider price lists change.
// Format: [inputUSDper1M, outputUSDper1M]. Mirrors the agent-side map in
// services/agent/pipeline/llm.py — keep both in sync until they share a
// generated source.
var PricePerMToken = map[string][2]float64{
	"gpt-4o":            {2.50, 10.00},
	"claude-sonnet-4-6": {3.00, 15.00},
}

// computeCostUSD returns the dollar cost for one LLM call.
// Returns 0.0 if the model is not in the pricing table (intentional — we want
// per-model spend to be visibly zero rather than a wrong estimate) and logs a
// warning so missing-pricing typos surface in service logs instead of silently
// under-reporting monthly spend.
func computeCostUSD(model string, promptTokens, completionTokens int32) float64 {
	rates, ok := PricePerMToken[model]
	if !ok {
		log.Printf("WARN computeCostUSD: model=%q not in PricePerMToken (prompt_tokens=%d, completion_tokens=%d) — returning 0.0", model, promptTokens, completionTokens)
		return 0.0
	}
	in := float64(promptTokens) / 1_000_000.0 * rates[0]
	out := float64(completionTokens) / 1_000_000.0 * rates[1]
	return in + out
}
