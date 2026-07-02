package engine

import (
	"fmt"

	"github.com/xxzhwl/gaia/components/workflow/domain"
)

// validateDefinitionStatusTransition 校验定义状态迁移是否合法。
//
// 允许的迁移：
//   - DEPLOYED → DISABLED（停用）
//   - DISABLED → DEPLOYED（重新启用）
//
// DRAFT 状态不能直接停用/启用，应先通过 DeployDefinition 部署。
func validateDefinitionStatusTransition(from, to domain.DefinitionStatus) error {
	switch {
	case from == domain.DefinitionStatusDeployed && to == domain.DefinitionStatusDisabled:
		return nil
	case from == domain.DefinitionStatusDisabled && to == domain.DefinitionStatusDeployed:
		return nil
	default:
		return fmt.Errorf("cannot transition definition from %s to %s", from, to)
	}
}
