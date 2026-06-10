package store

import (
	"context"
	"testing"
)

func TestSearchRequiresMinLength(t *testing.T) {
	s := &Store{}
	_, err := s.Search(context.Background(), "a", 10)
	if err != ErrBadInput {
		t.Fatalf("expected ErrBadInput, got %v", err)
	}
}
