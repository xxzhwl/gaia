package sla

import (
	"context"
	"errors"
	"testing"

	"github.com/xxzhwl/gaia/components/workflow/engine"
)

type fakeRuntime struct {
	result engine.ScanTimeoutTasksResult
	err    error
	req    engine.ScanTimeoutTasksRequest
}

func (r *fakeRuntime) ScanTimeoutTasks(_ context.Context, req engine.ScanTimeoutTasksRequest) (engine.ScanTimeoutTasksResult, error) {
	r.req = req
	return r.result, r.err
}

func TestScannerScanPassesBatchSize(t *testing.T) {
	runtime := &fakeRuntime{result: engine.ScanTimeoutTasksResult{Scanned: 3, TimedOut: 2}}
	scanner := New(runtime, WithBatchSize(20))

	result, err := scanner.Scan(context.Background())
	if err != nil {
		t.Fatalf("scan: %v", err)
	}
	if result.TimedOut != 2 || runtime.req.Limit != 20 || runtime.req.Now.IsZero() {
		t.Fatalf("unexpected scan result or request: result=%#v req=%#v", result, runtime.req)
	}
}

func TestScannerRunReturnsScanError(t *testing.T) {
	wantErr := errors.New("db unavailable")
	scanner := New(&fakeRuntime{err: wantErr})

	err := scanner.Run(context.Background())
	if !errors.Is(err, wantErr) {
		t.Fatalf("expected scan error, got %v", err)
	}
}
