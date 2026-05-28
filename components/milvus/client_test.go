package milvus

import (
	"context"
	"strings"
	"testing"
)

func TestNewClientRequiresAddress(t *testing.T) {
	_, err := NewClient(Config{})
	if err == nil {
		t.Fatal("expected error")
	}
	if !strings.Contains(err.Error(), "Address") {
		t.Fatalf("expected Address error, got %v", err)
	}
}

func TestSearchOptionValidation(t *testing.T) {
	c := &Client{}

	_, err := c.Search(context.Background(), SearchOption{})
	if err == nil || !strings.Contains(err.Error(), "CollectionName") {
		t.Fatalf("expected CollectionName error, got %v", err)
	}

	_, err = c.Search(context.Background(), SearchOption{
		CollectionName: "docs",
	})
	if err == nil || !strings.Contains(err.Error(), "VectorField") {
		t.Fatalf("expected VectorField error, got %v", err)
	}

	_, err = c.Search(context.Background(), SearchOption{
		CollectionName: "docs",
		VectorField:    "embedding",
	})
	if err == nil || !strings.Contains(err.Error(), "Vectors") {
		t.Fatalf("expected Vectors error, got %v", err)
	}

	_, err = c.Search(context.Background(), SearchOption{
		CollectionName: "docs",
		VectorField:    "embedding",
		Vectors:        [][]float32{{1, 2, 3}},
	})
	if err == nil || !strings.Contains(err.Error(), "TopK") {
		t.Fatalf("expected TopK error, got %v", err)
	}
}
