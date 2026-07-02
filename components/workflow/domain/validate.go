package domain

import (
	"fmt"
	"strings"
)

// ValidateDefinition 校验流程定义是否满足运行时最低结构要求。
func ValidateDefinition(def ProcessDefinition) error {
	if strings.TrimSpace(def.Key) == "" {
		return fmt.Errorf("definition key is required")
	}
	if strings.TrimSpace(def.Name) == "" {
		return fmt.Errorf("definition name is required")
	}
	if len(def.Model.Nodes) == 0 {
		return fmt.Errorf("definition must contain at least one node")
	}

	startCount := 0
	endCount := 0
	startNodeID := ""
	inbound := map[string]int{}
	outbound := map[string]int{}
	adjacency := map[string][]string{}
	for id, node := range def.Model.Nodes {
		if strings.TrimSpace(id) == "" || strings.TrimSpace(node.ID) == "" {
			return fmt.Errorf("node id is required")
		}
		if id != node.ID {
			return fmt.Errorf("node map key %q must equal node id %q", id, node.ID)
		}
		switch node.Type {
		case NodeTypeStartEvent:
			startCount++
			startNodeID = node.ID
		case NodeTypeEndEvent:
			endCount++
		case NodeTypeUserTask, NodeTypeServiceTask, NodeTypeExclusiveGateway, NodeTypeParallelGateway, NodeTypeInclusiveGateway:
		default:
			return fmt.Errorf("node %s has unsupported type %s", node.ID, node.Type)
		}
		if err := validateSLAPolicy(node); err != nil {
			return err
		}
	}
	if startCount != 1 {
		return fmt.Errorf("definition must contain exactly one start event, got %d", startCount)
	}
	if endCount == 0 {
		return fmt.Errorf("definition must contain at least one end event")
	}

	flowIDs := map[string]struct{}{}
	for _, flow := range def.Model.SequenceFlows {
		if strings.TrimSpace(flow.ID) == "" {
			return fmt.Errorf("sequence flow id is required")
		}
		if _, ok := flowIDs[flow.ID]; ok {
			return fmt.Errorf("duplicate sequence flow id %s", flow.ID)
		}
		flowIDs[flow.ID] = struct{}{}
		if _, ok := def.Model.Nodes[flow.SourceRef]; !ok {
			return fmt.Errorf("sequence flow %s source node %s not found", flow.ID, flow.SourceRef)
		}
		if _, ok := def.Model.Nodes[flow.TargetRef]; !ok {
			return fmt.Errorf("sequence flow %s target node %s not found", flow.ID, flow.TargetRef)
		}
		outbound[flow.SourceRef]++
		inbound[flow.TargetRef]++
		adjacency[flow.SourceRef] = append(adjacency[flow.SourceRef], flow.TargetRef)
	}
	if !canReachEnd(startNodeID, adjacency, def.Model.Nodes) {
		return fmt.Errorf("definition must have at least one path from start event to end event")
	}

	for _, node := range def.Model.Nodes {
		if node.Type != NodeTypeStartEvent && inbound[node.ID] == 0 {
			return fmt.Errorf("node %s has no inbound sequence flow", node.ID)
		}
		if node.Type == NodeTypeServiceTask && strings.TrimSpace(node.Endpoint) == "" {
			return fmt.Errorf("service task %s endpoint is required", node.ID)
		}
		if node.Type == NodeTypeParallelGateway {
			for _, flow := range def.Model.SequenceFlows {
				if flow.SourceRef != node.ID {
					continue
				}
				if strings.TrimSpace(flow.Condition) != "" || flow.Default {
					return fmt.Errorf("parallel gateway %s must not define conditions or default flow", node.ID)
				}
			}
		}
		if node.Type == NodeTypeExclusiveGateway || node.Type == NodeTypeInclusiveGateway {
			defaultCount := 0
			conditionalCount := 0
			for _, flow := range def.Model.SequenceFlows {
				if flow.SourceRef != node.ID {
					continue
				}
				if flow.Default {
					defaultCount++
				}
				if strings.TrimSpace(flow.Condition) != "" {
					conditionalCount++
				}
			}
			if defaultCount > 1 {
				return fmt.Errorf("%s %s can have at most one default flow", strings.ToLower(string(node.Type)), node.ID)
			}
			if outbound[node.ID] > 1 && conditionalCount == 0 && defaultCount == 0 {
				return fmt.Errorf("%s %s needs at least one condition or default flow", strings.ToLower(string(node.Type)), node.ID)
			}
		}
	}

	return nil
}

func canReachEnd(startNodeID string, adjacency map[string][]string, nodes map[string]Node) bool {
	visited := map[string]struct{}{}
	stack := []string{startNodeID}
	for len(stack) > 0 {
		nodeID := stack[len(stack)-1]
		stack = stack[:len(stack)-1]
		if _, ok := visited[nodeID]; ok {
			continue
		}
		visited[nodeID] = struct{}{}
		if nodes[nodeID].Type == NodeTypeEndEvent {
			return true
		}
		stack = append(stack, adjacency[nodeID]...)
	}
	return false
}

func validateSLAPolicy(node Node) error {
	if node.SLAPolicy.Action == "" {
		return nil
	}
	switch node.SLAPolicy.Action {
	case SLAActionNotify, SLAActionEscalate, SLAActionReject, SLAActionSuspend, SLAActionTerminate:
		return nil
	default:
		return fmt.Errorf("node %s has unsupported SLA action %s", node.ID, node.SLAPolicy.Action)
	}
}

// StartNode 返回流程模型中的开始节点。
func (m WorkflowModel) StartNode() (Node, bool) {
	for _, node := range m.Nodes {
		if node.Type == NodeTypeStartEvent {
			return node, true
		}
	}
	return Node{}, false
}

// Outgoing 返回指定节点的全部出向顺序流。
func (m WorkflowModel) Outgoing(nodeID string) []SequenceFlow {
	flows := make([]SequenceFlow, 0)
	for _, flow := range m.SequenceFlows {
		if flow.SourceRef == nodeID {
			flows = append(flows, flow)
		}
	}
	return flows
}

// Incoming 返回指定节点的全部入向顺序流。
func (m WorkflowModel) Incoming(nodeID string) []SequenceFlow {
	flows := make([]SequenceFlow, 0)
	for _, flow := range m.SequenceFlows {
		if flow.TargetRef == nodeID {
			flows = append(flows, flow)
		}
	}
	return flows
}
