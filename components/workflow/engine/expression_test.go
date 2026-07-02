package engine

import "testing"

func TestEvalConditionSupportsVariableRightOperand(t *testing.T) {
	matched, err := evalCondition("${amount >= threshold}", map[string]any{
		"amount":    1200,
		"threshold": 1000,
	})
	if err != nil {
		t.Fatalf("eval condition: %v", err)
	}
	if !matched {
		t.Fatal("expected variable-to-variable comparison to match")
	}
}

func TestConditionMatchesTreatsInvalidExpressionAsNotMatched(t *testing.T) {
	if conditionMatches("${amount >= }", map[string]any{"amount": 1200}) {
		t.Fatal("expected invalid expression to be treated as not matched")
	}
	if conditionMatches("${missing >= 10}", map[string]any{"amount": 1200}) {
		t.Fatal("expected missing variable to be treated as not matched")
	}
}
