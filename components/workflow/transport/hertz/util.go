package hertz

import (
	"strconv"
	"strings"

	"github.com/xxzhwl/gaia/framework/server"
)

// parseIntDefault 解析十进制整数，空串或非法值时返回 fallback。
func parseIntDefault(raw string, fallback int) int {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return fallback
	}
	value, err := strconv.Atoi(raw)
	if err != nil {
		return fallback
	}
	return value
}

func variableNamesFromRequest(req server.Request) []string {
	rawValues := req.GetUrlQueryArray("names")
	if single := strings.TrimSpace(req.GetUrlQuery("name")); single != "" {
		rawValues = append(rawValues, single)
	}
	names := make([]string, 0, len(rawValues))
	for _, raw := range rawValues {
		for _, part := range strings.Split(raw, ",") {
			name := strings.TrimSpace(part)
			if name != "" {
				names = append(names, name)
			}
		}
	}
	return names
}
