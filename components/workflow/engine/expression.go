package engine

import (
	"fmt"
	"regexp"
	"strconv"
	"strings"
)

var comparisonExpr = regexp.MustCompile(`^\s*\$\{\s*([A-Za-z_][A-Za-z0-9_.]*)\s*(==|!=|>=|<=|>|<)\s*(.+?)\s*\}\s*$`)

func evalCondition(expr string, vars map[string]any) (bool, error) {
	if strings.TrimSpace(expr) == "" {
		return true, nil
	}
	matches := comparisonExpr.FindStringSubmatch(expr)
	if len(matches) != 4 {
		return false, fmt.Errorf("unsupported condition expression %q", expr)
	}
	left, ok := vars[matches[1]]
	if !ok {
		return false, fmt.Errorf("variable %s not found for condition %q", matches[1], expr)
	}
	op := matches[2]
	rightLiteral := strings.TrimSpace(matches[3])
	right, err := parseLiteral(rightLiteral)
	if err != nil {
		return false, err
	}

	switch op {
	case "==":
		return fmt.Sprint(left) == fmt.Sprint(right), nil
	case "!=":
		return fmt.Sprint(left) != fmt.Sprint(right), nil
	case ">", ">=", "<", "<=":
		lf, lok := asFloat(left)
		rf, rok := asFloat(right)
		if !lok || !rok {
			return false, fmt.Errorf("condition %q requires numeric operands", expr)
		}
		switch op {
		case ">":
			return lf > rf, nil
		case ">=":
			return lf >= rf, nil
		case "<":
			return lf < rf, nil
		case "<=":
			return lf <= rf, nil
		}
	}
	return false, fmt.Errorf("unsupported operator %s", op)
}

func parseLiteral(raw string) (any, error) {
	if len(raw) >= 2 {
		if (raw[0] == '"' && raw[len(raw)-1] == '"') || (raw[0] == '\'' && raw[len(raw)-1] == '\'') {
			return raw[1 : len(raw)-1], nil
		}
	}
	switch strings.ToLower(raw) {
	case "true":
		return true, nil
	case "false":
		return false, nil
	}
	if f, err := strconv.ParseFloat(raw, 64); err == nil {
		return f, nil
	}
	return strings.TrimSpace(raw), nil
}

func asFloat(v any) (float64, bool) {
	switch val := v.(type) {
	case int:
		return float64(val), true
	case int8:
		return float64(val), true
	case int16:
		return float64(val), true
	case int32:
		return float64(val), true
	case int64:
		return float64(val), true
	case uint:
		return float64(val), true
	case uint8:
		return float64(val), true
	case uint16:
		return float64(val), true
	case uint32:
		return float64(val), true
	case uint64:
		return float64(val), true
	case float32:
		return float64(val), true
	case float64:
		return val, true
	case string:
		f, err := strconv.ParseFloat(val, 64)
		return f, err == nil
	default:
		return 0, false
	}
}
