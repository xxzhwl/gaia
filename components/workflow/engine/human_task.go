package engine

import (
	"fmt"
	"regexp"
	"strings"
	"time"

	"github.com/xxzhwl/gaia/components/workflow/domain"
)

var variableRefPattern = regexp.MustCompile(`\$\{\s*([A-Za-z_][A-Za-z0-9_.]*)\s*\}`)

func buildTaskFromNode(ids IDGenerator, now NowFunc, instanceID, activityID string, node domain.Node, taskType domain.TaskType, values map[string]any) domain.Task {
	task := domain.Task{
		ID:                 ids.Next("task"),
		InstanceID:         instanceID,
		ActivityInstanceID: activityID,
		NodeID:             node.ID,
		Title:              renderTaskTitle(node),
		Type:               taskType,
		Status:             domain.TaskStatusWaiting,
		Assignee:           renderExpression(node.AssigneeExpression, values),
		VariableSnapshot:   cloneAnyMap(values),
		DispatchURL:        node.Endpoint,
		CallbackToken:      ids.Next("cb"),
		RetryCount:         0,
		CreatedAt:          now(),
	}
	return task
}

// NowFunc 表示创建任务时读取当前时间的函数。
type NowFunc func() time.Time

func renderTaskTitle(node domain.Node) string {
	title := strings.TrimSpace(node.Name)
	if title == "" {
		title = node.ID
	}
	return title
}

func renderExpression(expression string, values map[string]any) string {
	expression = strings.TrimSpace(expression)
	if expression == "" {
		return ""
	}
	matches := variableRefPattern.FindAllStringSubmatch(expression, -1)
	if len(matches) == 1 && strings.TrimSpace(matches[0][0]) == expression {
		if value, ok := lookupVariable(values, matches[0][1]); ok {
			return fmt.Sprint(value)
		}
		return ""
	}
	return variableRefPattern.ReplaceAllStringFunc(expression, func(match string) string {
		parts := variableRefPattern.FindStringSubmatch(match)
		if len(parts) != 2 {
			return match
		}
		if value, ok := lookupVariable(values, parts[1]); ok {
			return fmt.Sprint(value)
		}
		return ""
	})
}

func lookupVariable(values map[string]any, key string) (any, bool) {
	if values == nil {
		return nil, false
	}
	if value, ok := values[key]; ok {
		return value, true
	}
	parts := strings.Split(key, ".")
	var current any = values
	for _, part := range parts {
		next, ok := current.(map[string]any)
		if !ok {
			return nil, false
		}
		current, ok = next[part]
		if !ok {
			return nil, false
		}
	}
	return current, true
}

func cloneAnyMap(input map[string]any) map[string]any {
	if input == nil {
		return nil
	}
	result := make(map[string]any, len(input))
	for key, value := range input {
		result[key] = value
	}
	return result
}

func taskAuditPayload(task domain.Task) map[string]any {
	payload := map[string]any{
		"taskType": task.Type,
		"title":    task.Title,
		"assignee": task.Assignee,
	}
	if task.Action != "" {
		payload["action"] = task.Action
	}
	if task.Comment != "" {
		payload["comment"] = task.Comment
	}
	if task.CompletedBy != "" {
		payload["completedBy"] = task.CompletedBy
	}
	if task.Owner != "" {
		payload["operator"] = task.Owner
	}
	if task.DelegatedFrom != "" {
		payload["delegatedFrom"] = task.DelegatedFrom
	}
	return payload
}
