package workflow

import (
	"context"
	"sync"
	"time"

	"github.com/xxzhwl/gaia/components/workflow/domain"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/metric"
	"go.opentelemetry.io/otel/trace"
)

// MetricsSnapshot 表示 workflow 组件当前可观测状态的聚合快照。
type MetricsSnapshot struct {
	OutboxBacklog     int64
	OutboxDeadLetters int64
	InstanceStatus    map[domain.InstanceStatus]int64
}

// WorkflowMetrics 持有 workflow 组件的 OTel 指标。
type WorkflowMetrics struct {
	OperationTotal    metric.Int64Counter
	OperationDuration metric.Float64Histogram
	SLATimeoutTotal   metric.Int64Counter

	OutboxBacklog     metric.Int64ObservableGauge
	OutboxDeadLetters metric.Int64ObservableGauge
	InstanceStatus    metric.Int64ObservableGauge

	providersMu sync.Mutex
	provider    func(context.Context) (MetricsSnapshot, error)
}

// WorkflowMetricAttr 为 workflow 指标提供统一标签键。
var WorkflowMetricAttr = struct {
	Operation attribute.Key
	Status    attribute.Key
}{
	Operation: attribute.Key("operation"),
	Status:    attribute.Key("status"),
}

var (
	workflowMetricsOnce   sync.Once
	workflowMetricsGlobal *WorkflowMetrics
	workflowTracer        = otel.Tracer("github.com/xxzhwl/gaia/components/workflow", trace.WithInstrumentationVersion("1.0.0"))
)

// GetWorkflowMetrics 获取 workflow 全局指标对象。
func GetWorkflowMetrics() *WorkflowMetrics {
	workflowMetricsOnce.Do(func() {
		meter := otel.Meter("github.com/xxzhwl/gaia/components/workflow",
			metric.WithInstrumentationVersion("1.0.0"),
		)
		m := &WorkflowMetrics{}
		var err error

		m.OperationTotal, err = meter.Int64Counter("workflow.operation.total",
			metric.WithDescription("Workflow engine operation calls by operation and status"))
		otel.Handle(err)

		m.OperationDuration, err = meter.Float64Histogram("workflow.operation.duration",
			metric.WithDescription("Workflow engine operation duration in milliseconds"),
			metric.WithUnit("ms"))
		otel.Handle(err)

		m.SLATimeoutTotal, err = meter.Int64Counter("workflow.sla.timeout.total",
			metric.WithDescription("Workflow SLA timeout events detected by ScanTimeoutTasks"))
		otel.Handle(err)

		m.OutboxBacklog, err = meter.Int64ObservableGauge("workflow.outbox.backlog",
			metric.WithDescription("Workflow outbox events not yet completed (NEW, FAILED, PROCESSING)"))
		otel.Handle(err)

		m.OutboxDeadLetters, err = meter.Int64ObservableGauge("workflow.outbox.dead_letters",
			metric.WithDescription("Workflow outbox events in DEAD status"))
		otel.Handle(err)

		m.InstanceStatus, err = meter.Int64ObservableGauge("workflow.instance.status.count",
			metric.WithDescription("Workflow process instance count by status"))
		otel.Handle(err)

		_, err = meter.RegisterCallback(func(ctx context.Context, observer metric.Observer) error {
			m.providersMu.Lock()
			provider := m.provider
			m.providersMu.Unlock()
			if provider == nil {
				return nil
			}
			snapshot, err := provider(ctx)
			if err != nil {
				return nil
			}
			observer.ObserveInt64(m.OutboxBacklog, snapshot.OutboxBacklog)
			observer.ObserveInt64(m.OutboxDeadLetters, snapshot.OutboxDeadLetters)
			for status, count := range snapshot.InstanceStatus {
				observer.ObserveInt64(m.InstanceStatus, count, metric.WithAttributes(
					WorkflowMetricAttr.Status.String(string(status)),
				))
			}
			return nil
		}, m.OutboxBacklog, m.OutboxDeadLetters, m.InstanceStatus)
		otel.Handle(err)

		workflowMetricsGlobal = m
	})
	return workflowMetricsGlobal
}

func registerWorkflowMetricsProvider(provider func(context.Context) (MetricsSnapshot, error)) {
	if provider == nil {
		return
	}
	m := GetWorkflowMetrics()
	m.providersMu.Lock()
	defer m.providersMu.Unlock()
	m.provider = provider
}

func startWorkflowOperation(ctx context.Context, operation string) (context.Context, func(error)) {
	ctx, span := workflowTracer.Start(ctx, "workflow."+operation,
		trace.WithSpanKind(trace.SpanKindInternal),
		trace.WithAttributes(WorkflowMetricAttr.Operation.String(operation)),
	)
	start := time.Now()
	return ctx, func(err error) {
		statusValue := "success"
		if err != nil {
			statusValue = "error"
			span.RecordError(err)
			span.SetStatus(codes.Error, err.Error())
		} else {
			span.SetStatus(codes.Ok, "")
		}
		attrs := metric.WithAttributes(
			WorkflowMetricAttr.Operation.String(operation),
			WorkflowMetricAttr.Status.String(statusValue),
		)
		m := GetWorkflowMetrics()
		if m.OperationTotal != nil {
			m.OperationTotal.Add(ctx, 1, attrs)
		}
		if m.OperationDuration != nil {
			m.OperationDuration.Record(ctx, float64(time.Since(start).Microseconds())/1000.0, attrs)
		}
		span.End()
	}
}

func recordSLATimeouts(ctx context.Context, count int) {
	if count <= 0 {
		return
	}
	m := GetWorkflowMetrics()
	if m.SLATimeoutTotal != nil {
		m.SLATimeoutTotal.Add(ctx, int64(count))
	}
}

func observeWorkflowOperation[T any](ctx context.Context, operation string, fn func(context.Context) (T, error)) (result T, err error) {
	ctx, end := startWorkflowOperation(ctx, operation)
	defer func() { end(err) }()
	return fn(ctx)
}

// SnapshotMetrics 聚合当前 workflow 运行时状态，供 OTel observable gauge 采样。
func (e *Engine) SnapshotMetrics(ctx context.Context) (MetricsSnapshot, error) {
	if e == nil || e.runtime == nil {
		return MetricsSnapshot{}, nil
	}
	snapshot := MetricsSnapshot{InstanceStatus: map[domain.InstanceStatus]int64{}}
	for _, status := range []domain.OutboxStatus{
		domain.OutboxStatusNew,
		domain.OutboxStatusProcessing,
		domain.OutboxStatusFailed,
	} {
		result, err := e.runtime.OutboxEvents(ctx, domain.OutboxListFilter{
			PageRequest: domain.PageRequest{Page: 1, PageSize: 1},
			Status:      status,
		})
		if err != nil {
			return MetricsSnapshot{}, err
		}
		snapshot.OutboxBacklog += result.Total
	}
	dead, err := e.runtime.OutboxEvents(ctx, domain.OutboxListFilter{
		PageRequest: domain.PageRequest{Page: 1, PageSize: 1},
		Status:      domain.OutboxStatusDead,
	})
	if err != nil {
		return MetricsSnapshot{}, err
	}
	snapshot.OutboxDeadLetters = dead.Total

	for _, status := range []domain.InstanceStatus{
		domain.InstanceStatusRunning,
		domain.InstanceStatusCompleted,
		domain.InstanceStatusFailed,
		domain.InstanceStatusTerminated,
		domain.InstanceStatusSuspended,
	} {
		result, err := e.runtime.ListInstances(ctx, domain.InstanceListFilter{
			PageRequest: domain.PageRequest{Page: 1, PageSize: 1},
			Status:      status,
		})
		if err != nil {
			return MetricsSnapshot{}, err
		}
		snapshot.InstanceStatus[status] = result.Total
	}
	return snapshot, nil
}
