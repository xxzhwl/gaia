package engine

import (
	"encoding/json"
	"fmt"
	"regexp"
	"strings"

	"github.com/xxzhwl/gaia/components/workflow/domain"
)

var variableRefExpr = regexp.MustCompile(`^\s*\$\{\s*([A-Za-z_][A-Za-z0-9_.]*)\s*\}\s*$`)

func buildInputPayload(node domain.Node, values map[string]any) (map[string]any, error) {
	payload := map[string]any{}
	for _, mapping := range node.InputMappings {
		parameter := strings.TrimSpace(mapping.Parameter)
		if parameter == "" {
			continue
		}
		value, err := resolveInputExpression(mapping.Expression, values)
		if err != nil {
			return nil, fmt.Errorf("resolve input %s: %w", parameter, err)
		}
		payload[parameter] = value
	}
	return payload, nil
}

func collectInputVariableRefs(node domain.Node) ([]string, error) {
	seen := map[string]struct{}{}
	var refs []string
	add := func(name string) {
		name = strings.TrimSpace(name)
		if name == "" {
			return
		}
		if _, ok := seen[name]; ok {
			return
		}
		seen[name] = struct{}{}
		refs = append(refs, name)
	}
	for _, mapping := range node.InputMappings {
		if err := collectExpressionRefs(mapping.Expression, add); err != nil {
			return nil, fmt.Errorf("collect input %s refs: %w", strings.TrimSpace(mapping.Parameter), err)
		}
	}
	return refs, nil
}

func collectExpressionRefs(expression string, add func(string)) error {
	expression = strings.TrimSpace(expression)
	if expression == "" {
		return nil
	}
	if name, ok := extractVariableRef(expression); ok {
		add(name)
		return nil
	}
	if strings.HasPrefix(expression, "{") || strings.HasPrefix(expression, "[") {
		var decoded any
		if err := json.Unmarshal([]byte(expression), &decoded); err != nil {
			return err
		}
		collectJSONRefs(decoded, add)
	}
	return nil
}

func collectJSONRefs(value any, add func(string)) {
	switch typed := value.(type) {
	case map[string]any:
		for _, item := range typed {
			collectJSONRefs(item, add)
		}
	case []any:
		for _, item := range typed {
			collectJSONRefs(item, add)
		}
	case string:
		if name, ok := extractVariableRef(typed); ok {
			add(name)
		}
	}
}

func resolveInputExpression(expression string, values map[string]any) (any, error) {
	expression = strings.TrimSpace(expression)
	if expression == "" {
		return nil, nil
	}
	if name, ok := extractVariableRef(expression); ok {
		value, exists := values[name]
		if !exists {
			return nil, fmt.Errorf("variable %s not found", name)
		}
		return value, nil
	}
	if strings.HasPrefix(expression, "{") || strings.HasPrefix(expression, "[") {
		var decoded any
		if err := json.Unmarshal([]byte(expression), &decoded); err != nil {
			return nil, err
		}
		return resolveJSONRefs(decoded, values)
	}
	return expression, nil
}

func resolveJSONRefs(value any, values map[string]any) (any, error) {
	switch typed := value.(type) {
	case map[string]any:
		for key, item := range typed {
			resolved, err := resolveJSONRefs(item, values)
			if err != nil {
				return nil, err
			}
			typed[key] = resolved
		}
		return typed, nil
	case []any:
		for i, item := range typed {
			resolved, err := resolveJSONRefs(item, values)
			if err != nil {
				return nil, err
			}
			typed[i] = resolved
		}
		return typed, nil
	case string:
		if name, ok := extractVariableRef(typed); ok {
			value, exists := values[name]
			if !exists {
				return nil, fmt.Errorf("variable %s not found", name)
			}
			return value, nil
		}
		return typed, nil
	default:
		return typed, nil
	}
}

func extractVariableRef(expression string) (string, bool) {
	matches := variableRefExpr.FindStringSubmatch(expression)
	if len(matches) != 2 {
		return "", false
	}
	return matches[1], true
}
