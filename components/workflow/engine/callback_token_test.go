package engine

import (
	"testing"

	"github.com/xxzhwl/gaia/components/workflow/domain"
)

func TestVerifyCallbackTokenSkipsWhenNotProvided(t *testing.T) {
	task := domain.Task{ID: "task_1", CallbackToken: "cb_secret"}
	if err := verifyCallbackToken(task, ""); err != nil {
		t.Fatalf("expected trusted path (no token) to pass, got %v", err)
	}
}

func TestVerifyCallbackTokenAcceptsMatch(t *testing.T) {
	task := domain.Task{ID: "task_1", CallbackToken: "cb_secret"}
	if err := verifyCallbackToken(task, "cb_secret"); err != nil {
		t.Fatalf("expected matching token to pass, got %v", err)
	}
}

func TestVerifyCallbackTokenRejectsMismatch(t *testing.T) {
	task := domain.Task{ID: "task_1", CallbackToken: "cb_secret"}
	if err := verifyCallbackToken(task, "cb_wrong"); err == nil {
		t.Fatal("expected mismatching token to be rejected")
	}
}

func TestVerifyCallbackTokenRejectsWhenTaskHasNoToken(t *testing.T) {
	task := domain.Task{ID: "task_1"}
	if err := verifyCallbackToken(task, "cb_anything"); err == nil {
		t.Fatal("expected rejection when task has no callback token but one is provided")
	}
}

func TestNewCallbackTokenIsUnpredictable(t *testing.T) {
	ids := &SequenceIDGenerator{}
	a := newCallbackToken(ids)
	b := newCallbackToken(ids)
	if a == b {
		t.Fatalf("expected distinct tokens, got %q twice", a)
	}
	if len(a) < 16 {
		t.Fatalf("expected sufficiently long token, got %q", a)
	}
}
