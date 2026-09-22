package helps

import (
	"sync"

	"github.com/router-for-me/CLIProxyAPI/v7/internal/registry"
)

var cursorRoutingCatalogs sync.Map

// StoreCursorRoutingModels retains the catalog after exclusions and before aliases,
// so family resolution cannot select an excluded upstream variant.
func StoreCursorRoutingModels(authID string, models []*registry.ModelInfo) {
	cursorRoutingCatalogs.Store(authID, cloneCursorRoutingModels(models))
}

func CursorRoutingModels(authID string, fallback []*registry.ModelInfo) []*registry.ModelInfo {
	if models, ok := cursorRoutingCatalogs.Load(authID); ok {
		return cloneCursorRoutingModels(models.([]*registry.ModelInfo))
	}
	return fallback
}

func cloneCursorRoutingModels(models []*registry.ModelInfo) []*registry.ModelInfo {
	result := make([]*registry.ModelInfo, 0, len(models))
	for _, model := range models {
		if model != nil {
			copy := *model
			result = append(result, &copy)
		}
	}
	return result
}
