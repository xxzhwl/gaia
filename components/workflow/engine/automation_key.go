package engine

import (
	"strings"

	"github.com/xxzhwl/gaia/components/workflow/automation"
	"github.com/xxzhwl/gaia/components/workflow/domain"
)

func automationTaskKey(node domain.Node) string {
	if key := strings.TrimSpace(node.AutomationTaskKey); key != "" {
		return key
	}
	if strings.HasPrefix(strings.TrimSpace(node.Endpoint), "gaia://") {
		if _, taskKey, err := automation.ParseGaiaRef(node.Endpoint); err == nil {
			return taskKey
		}
	}
	return strings.TrimSpace(node.ID)
}
