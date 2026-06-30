package testfixture

import "github.com/xxzhwl/gaia/components/workflow/domain"

// OrderApprovalDefinition 返回订单审批测试流程定义。
func OrderApprovalDefinition(serviceEndpoint string) domain.ProcessDefinition {
	return domain.ProcessDefinition{
		Key:  "order_approval",
		Name: "Order Approval",
		Inputs: []domain.InputParameter{
			{Name: "orderId", Type: "string", Required: true},
		},
		Model: domain.WorkflowModel{
			Nodes: map[string]domain.Node{
				"start": {
					ID:   "start",
					Type: domain.NodeTypeStartEvent,
					Name: "Start",
				},
				"manager_approve": {
					ID:                 "manager_approve",
					Type:               domain.NodeTypeUserTask,
					Name:               "Manager Approve",
					AssigneeExpression: "${starter.managerId}",
					OutputVariables:    []string{"approvalResult"},
				},
				"approval_gateway": {
					ID:   "approval_gateway",
					Type: domain.NodeTypeExclusiveGateway,
					Name: "Approval Gateway",
				},
				"send_contract": {
					ID:              "send_contract",
					Type:            domain.NodeTypeServiceTask,
					Name:            "Send Contract",
					Endpoint:        serviceEndpoint,
					InputMappings:   inputMappings("orderId"),
					OutputVariables: []string{"contractId"},
					TimeoutSeconds:  300,
				},
				"end_approved": {
					ID:   "end_approved",
					Type: domain.NodeTypeEndEvent,
					Name: "Approved End",
				},
				"end_rejected": {
					ID:   "end_rejected",
					Type: domain.NodeTypeEndEvent,
					Name: "Rejected End",
				},
			},
			SequenceFlows: []domain.SequenceFlow{
				{ID: "flow_start_approve", SourceRef: "start", TargetRef: "manager_approve"},
				{ID: "flow_approve_gateway", SourceRef: "manager_approve", TargetRef: "approval_gateway"},
				{ID: "flow_approved", SourceRef: "approval_gateway", TargetRef: "send_contract", Condition: `${approvalResult == "APPROVED"}`},
				{ID: "flow_rejected", SourceRef: "approval_gateway", TargetRef: "end_rejected", Default: true},
				{ID: "flow_contract_end", SourceRef: "send_contract", TargetRef: "end_approved"},
			},
		},
	}
}

// ComplexAutomationDefinition 返回覆盖多网关和自动化节点的测试流程定义。
func ComplexAutomationDefinition(workerEndpoint string) domain.ProcessDefinition {
	return domain.ProcessDefinition{
		Key:  "complex_automation_order",
		Name: "Complex Automation Order",
		Inputs: []domain.InputParameter{
			{Name: "orderId", Type: "string", Required: true},
			{Name: "amount", Type: "number", Required: true},
			{Name: "customerLevel", Type: "string", Required: true},
		},
		Model: domain.WorkflowModel{
			Nodes: map[string]domain.Node{
				"start": {ID: "start", Type: domain.NodeTypeStartEvent, Name: "Start"},
				"normalize_order": {
					ID:                "normalize_order",
					Type:              domain.NodeTypeServiceTask,
					Name:              "Normalize Order",
					Endpoint:          workerEndpoint,
					AutomationTaskKey: "normalize_order",
					InputMappings:     inputMappings("orderId", "amount", "customerLevel"),
					OutputVariables:   []string{"riskLevel", "vip", "needCompliance", "normalizedAmount", "autoApproved"},
				},
				"risk_gateway": {
					ID:   "risk_gateway",
					Type: domain.NodeTypeExclusiveGateway,
					Name: "Risk Decision",
				},
				"mark_high_risk": {
					ID:                "mark_high_risk",
					Type:              domain.NodeTypeServiceTask,
					Name:              "Mark High Risk",
					Endpoint:          workerEndpoint,
					AutomationTaskKey: "mark_high_risk",
					InputMappings:     inputMappings("orderId", "riskLevel"),
					OutputVariables:   []string{"orderStatus", "rejectReason"},
				},
				"parallel_split": {
					ID:   "parallel_split",
					Type: domain.NodeTypeParallelGateway,
					Name: "Parallel Split",
				},
				"reserve_inventory": {
					ID:                "reserve_inventory",
					Type:              domain.NodeTypeServiceTask,
					Name:              "Reserve Inventory",
					Endpoint:          workerEndpoint,
					AutomationTaskKey: "reserve_inventory",
					InputMappings:     inputMappings("orderId", "amount"),
					OutputVariables:   []string{"inventoryReserved", "inventoryReservationId", "warehouseCode"},
				},
				"issue_invoice": {
					ID:                "issue_invoice",
					Type:              domain.NodeTypeServiceTask,
					Name:              "Issue Invoice",
					Endpoint:          workerEndpoint,
					AutomationTaskKey: "issue_invoice",
					InputMappings:     inputMappings("orderId", "amount"),
					OutputVariables:   []string{"invoiceId", "invoiceAmount"},
				},
				"parallel_join": {
					ID:   "parallel_join",
					Type: domain.NodeTypeParallelGateway,
					Name: "Parallel Join",
				},
				"inclusive_split": {
					ID:   "inclusive_split",
					Type: domain.NodeTypeInclusiveGateway,
					Name: "Optional Actions",
				},
				"vip_reward": {
					ID:                "vip_reward",
					Type:              domain.NodeTypeServiceTask,
					Name:              "VIP Reward",
					Endpoint:          workerEndpoint,
					AutomationTaskKey: "vip_reward",
					InputMappings:     inputMappings("orderId", "customerLevel", "vip"),
					OutputVariables:   []string{"rewardGranted", "rewardTier"},
				},
				"compliance_archive": {
					ID:                "compliance_archive",
					Type:              domain.NodeTypeServiceTask,
					Name:              "Compliance Archive",
					Endpoint:          workerEndpoint,
					AutomationTaskKey: "compliance_archive",
					InputMappings:     inputMappings("orderId", "amount", "needCompliance"),
					OutputVariables:   []string{"archived", "archiveId"},
				},
				"standard_notify": {
					ID:                "standard_notify",
					Type:              domain.NodeTypeServiceTask,
					Name:              "Standard Notify",
					Endpoint:          workerEndpoint,
					AutomationTaskKey: "standard_notify",
					InputMappings:     inputMappings("orderId"),
					OutputVariables:   []string{"notificationSent"},
				},
				"inclusive_join": {
					ID:   "inclusive_join",
					Type: domain.NodeTypeInclusiveGateway,
					Name: "Optional Join",
				},
				"settle_order": {
					ID:                "settle_order",
					Type:              domain.NodeTypeServiceTask,
					Name:              "Settle Order",
					Endpoint:          workerEndpoint,
					AutomationTaskKey: "settle_order",
					InputMappings:     inputMappings("orderId", "inventoryReserved", "invoiceId", "rewardGranted", "archived"),
					OutputVariables:   []string{"orderStatus", "fulfillmentReady"},
				},
				"end_success": {
					ID:   "end_success",
					Type: domain.NodeTypeEndEvent,
					Name: "Success End",
				},
				"end_high_risk": {
					ID:   "end_high_risk",
					Type: domain.NodeTypeEndEvent,
					Name: "High Risk End",
				},
			},
			SequenceFlows: []domain.SequenceFlow{
				{ID: "flow_start_normalize", Name: "start", SourceRef: "start", TargetRef: "normalize_order"},
				{ID: "flow_normalize_risk", Name: "risk check", SourceRef: "normalize_order", TargetRef: "risk_gateway"},
				{ID: "flow_high_risk", Name: "high risk", SourceRef: "risk_gateway", TargetRef: "mark_high_risk", Condition: `${riskLevel == "HIGH"}`},
				{ID: "flow_auto_path", Name: "auto path", SourceRef: "risk_gateway", TargetRef: "parallel_split", Default: true},
				{ID: "flow_high_risk_end", Name: "reject", SourceRef: "mark_high_risk", TargetRef: "end_high_risk"},
				{ID: "flow_split_inventory", Name: "inventory", SourceRef: "parallel_split", TargetRef: "reserve_inventory"},
				{ID: "flow_split_invoice", Name: "invoice", SourceRef: "parallel_split", TargetRef: "issue_invoice"},
				{ID: "flow_inventory_join", SourceRef: "reserve_inventory", TargetRef: "parallel_join"},
				{ID: "flow_invoice_join", SourceRef: "issue_invoice", TargetRef: "parallel_join"},
				{ID: "flow_parallel_inclusive", SourceRef: "parallel_join", TargetRef: "inclusive_split"},
				{ID: "flow_vip_reward", Name: "vip", SourceRef: "inclusive_split", TargetRef: "vip_reward", Condition: `${vip == true}`},
				{ID: "flow_compliance", Name: "compliance", SourceRef: "inclusive_split", TargetRef: "compliance_archive", Condition: `${needCompliance == true}`},
				{ID: "flow_standard_notify", Name: "default notify", SourceRef: "inclusive_split", TargetRef: "standard_notify", Default: true},
				{ID: "flow_vip_join", SourceRef: "vip_reward", TargetRef: "inclusive_join"},
				{ID: "flow_compliance_join", SourceRef: "compliance_archive", TargetRef: "inclusive_join"},
				{ID: "flow_notify_join", SourceRef: "standard_notify", TargetRef: "inclusive_join"},
				{ID: "flow_optional_settle", SourceRef: "inclusive_join", TargetRef: "settle_order"},
				{ID: "flow_settle_end", SourceRef: "settle_order", TargetRef: "end_success"},
			},
		},
	}
}

func inputMappings(names ...string) []domain.InputMapping {
	mappings := make([]domain.InputMapping, 0, len(names))
	for _, name := range names {
		mappings = append(mappings, domain.InputMapping{
			Parameter:  name,
			Expression: "${" + name + "}",
		})
	}
	return mappings
}
