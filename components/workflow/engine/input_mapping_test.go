package engine

import (
	"reflect"
	"testing"

	"github.com/xxzhwl/gaia/components/workflow/domain"
)

func TestCollectInputVariableRefsFromMappings(t *testing.T) {
	node := domain.Node{
		InputMappings: []domain.InputMapping{
			{Parameter: "orderId", Expression: "${orderId}"},
			{Parameter: "payload", Expression: `{"id":"${orderId}","items":["${items}"],"literal":"orderId"}`},
			{Parameter: "amount", Expression: "${amount}"},
			{Parameter: "constant", Expression: "literal"},
		},
	}

	got, err := collectInputVariableRefs(node)
	if err != nil {
		t.Fatalf("collectInputVariableRefs() error = %v", err)
	}
	want := []string{"orderId", "items", "amount"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("unexpected refs: got %#v want %#v", got, want)
	}
}

func TestCollectInputVariableRefsReturnsJSONErrors(t *testing.T) {
	_, err := collectInputVariableRefs(domain.Node{
		InputMappings: []domain.InputMapping{{Parameter: "payload", Expression: `{"id":`}},
	})
	if err == nil {
		t.Fatal("expected invalid JSON expression to be rejected")
	}
}

func TestBuildInputPayloadDoesNotRequireUnusedVariables(t *testing.T) {
	node := domain.Node{
		InputMappings: []domain.InputMapping{
			{Parameter: "orderId", Expression: "${orderId}"},
			{Parameter: "literal", Expression: "amount"},
		},
	}
	payload, err := buildInputPayload(node, map[string]any{"orderId": "O-1"})
	if err != nil {
		t.Fatalf("buildInputPayload() error = %v", err)
	}
	if payload["orderId"] != "O-1" || payload["literal"] != "amount" {
		t.Fatalf("unexpected payload: %#v", payload)
	}
}
