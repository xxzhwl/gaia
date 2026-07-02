package domain

import "testing"

func TestValidateDefinitionRequiresExactlyOneStartEvent(t *testing.T) {
	def := ProcessDefinition{
		Key:  "order_approval",
		Name: "Order Approval",
		Model: WorkflowModel{
			Nodes: map[string]Node{
				"start_a": {ID: "start_a", Type: NodeTypeStartEvent},
				"start_b": {ID: "start_b", Type: NodeTypeStartEvent},
				"end":     {ID: "end", Type: NodeTypeEndEvent},
			},
			SequenceFlows: []SequenceFlow{
				{ID: "flow_a", SourceRef: "start_a", TargetRef: "end"},
				{ID: "flow_b", SourceRef: "start_b", TargetRef: "end"},
			},
		},
	}

	if err := ValidateDefinition(def); err == nil {
		t.Fatal("expected validation error for multiple start events")
	}
}

func TestValidateDefinitionRejectsServiceTaskWithoutEndpoint(t *testing.T) {
	def := ProcessDefinition{
		Key:  "order_approval",
		Name: "Order Approval",
		Model: WorkflowModel{
			Nodes: map[string]Node{
				"start":   {ID: "start", Type: NodeTypeStartEvent},
				"service": {ID: "service", Type: NodeTypeServiceTask},
				"end":     {ID: "end", Type: NodeTypeEndEvent},
			},
			SequenceFlows: []SequenceFlow{
				{ID: "flow_start_service", SourceRef: "start", TargetRef: "service"},
				{ID: "flow_service_end", SourceRef: "service", TargetRef: "end"},
			},
		},
	}

	if err := ValidateDefinition(def); err == nil {
		t.Fatal("expected validation error for service task without endpoint")
	}
}

func TestValidateDefinitionRejectsUnsupportedSLAAction(t *testing.T) {
	def := ProcessDefinition{
		Key:  "sla_invalid",
		Name: "SLA Invalid",
		Model: WorkflowModel{
			Nodes: map[string]Node{
				"start": {ID: "start", Type: NodeTypeStartEvent},
				"todo": {
					ID:        "todo",
					Type:      NodeTypeUserTask,
					SLAPolicy: SLAPolicy{Action: SLAAction("ARCHIVE")},
				},
				"end": {ID: "end", Type: NodeTypeEndEvent},
			},
			SequenceFlows: []SequenceFlow{
				{ID: "flow_start_todo", SourceRef: "start", TargetRef: "todo"},
				{ID: "flow_todo_end", SourceRef: "todo", TargetRef: "end"},
			},
		},
	}

	if err := ValidateDefinition(def); err == nil {
		t.Fatal("expected validation error for unsupported SLA action")
	}
}

func TestValidateDefinitionAllowsReachableDeadEndBranch(t *testing.T) {
	def := ProcessDefinition{
		Key:  "branch_dead_end",
		Name: "Branch Dead End",
		Model: WorkflowModel{
			Nodes: map[string]Node{
				"start": {ID: "start", Type: NodeTypeStartEvent},
				"split": {ID: "split", Type: NodeTypeParallelGateway},
				"todo":  {ID: "todo", Type: NodeTypeUserTask},
				"end":   {ID: "end", Type: NodeTypeEndEvent},
			},
			SequenceFlows: []SequenceFlow{
				{ID: "flow_start_split", SourceRef: "start", TargetRef: "split"},
				{ID: "flow_split_todo", SourceRef: "split", TargetRef: "todo"},
				{ID: "flow_split_end", SourceRef: "split", TargetRef: "end"},
			},
		},
	}

	if err := ValidateDefinition(def); err != nil {
		t.Fatalf("expected reachable dead-end branch to be valid: %v", err)
	}
}

func TestValidateDefinitionRequiresReachableEndEvent(t *testing.T) {
	def := ProcessDefinition{
		Key:  "no_end_path",
		Name: "No End Path",
		Model: WorkflowModel{
			Nodes: map[string]Node{
				"start": {ID: "start", Type: NodeTypeStartEvent},
				"todo":  {ID: "todo", Type: NodeTypeUserTask},
				"end":   {ID: "end", Type: NodeTypeEndEvent},
			},
			SequenceFlows: []SequenceFlow{
				{ID: "flow_start_todo", SourceRef: "start", TargetRef: "todo"},
			},
		},
	}

	if err := ValidateDefinition(def); err == nil {
		t.Fatal("expected validation error when no end event is reachable")
	}
}
