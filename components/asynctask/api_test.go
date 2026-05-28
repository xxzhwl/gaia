package asynctask

import "testing"

func TestCalculateP99(t *testing.T) {
	values := []int64{50, 10, 30, 20, 40}

	if got := calculateP99(values); got != 50 {
		t.Fatalf("calculateP99() = %d, want 50", got)
	}
}

func TestEscapeLikePattern(t *testing.T) {
	got := escapeLikePattern(`100%\_done`)
	want := `100\%\\\_done`
	if got != want {
		t.Fatalf("escapeLikePattern() = %q, want %q", got, want)
	}
}

func TestSubmitTaskRequiresScheduler(t *testing.T) {
	if _, err := SubmitTask("missing-scheduler", TaskBaseInfo{}); err == nil {
		t.Fatal("SubmitTask() expected error when scheduler does not exist")
	}
}
