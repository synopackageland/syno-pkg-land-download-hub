package gopeed

import (
	"context"
	"encoding/json"

	appapi "dsmapp/internal/api"
	"github.com/synopackageland/syno-pkg-land-sdk/api"
)

// These non-mutating plans never access resources or echo caller input. The real
// handler checks ownership, grants, freshness and consent at human execution.
// Route names are literal so the SDK entry audit can verify registrations.
func previewRoutes() []api.Route {
	var registry struct {
		Operations map[string]struct{ AI, Effect, Preview string }
	}
	if err := json.Unmarshal(appapi.Operations(), &registry); err != nil {
		panic("invalid embedded operation registry")
	}
	handler := func(preview string) api.Handler {
		return func(_ context.Context, _ api.Caller, raw json.RawMessage) (any, error) {
			var input map[string]json.RawMessage
			if json.Unmarshal(raw, &input) != nil || input == nil {
				return nil, refused("invalid_request")
			}
			for name, op := range registry.Operations {
				if op.AI == "draft" && op.Preview == preview {
					return map[string]any{"operation": name, "effect": op.Effect, "confirmationRequired": true, "checks": "execution_required"}, nil
				}
			}
			return nil, refused("endpoint_denied")
		}
	}
	return []api.Route{
		api.Handle("tasks.pause.preview", handler("tasks.pause.preview")),
		api.Handle("tasks.resume.preview", handler("tasks.resume.preview")),
		api.Handle("downloads.add.preview", handler("downloads.add.preview")),
		api.Handle("search.download.preview", handler("search.download.preview")),
		api.Handle("downloads.add_torrent.preview", handler("downloads.add_torrent.preview")),
		api.Handle("search.sources.save.preview", handler("search.sources.save.preview")),
		api.Handle("search.sources.patch.preview", handler("search.sources.patch.preview")),
		api.Handle("search.sources.field.preview", handler("search.sources.field.preview")),
		api.Handle("search.sources.enabled.preview", handler("search.sources.enabled.preview")),
		api.Handle("search.sources.delete.preview", handler("search.sources.delete.preview")),
		api.Handle("search.solver.set.preview", handler("search.solver.set.preview")),
		api.Handle("tasks.delete.preview", handler("tasks.delete.preview")),
		api.Handle("gopeed.destination.preview", handler("gopeed.destination.preview")),
		api.Handle("gopeed.plugins.switch.preview", handler("gopeed.plugins.switch.preview")),
		api.Handle("gopeed.plugins.delete.preview", handler("gopeed.plugins.delete.preview")),
		api.Handle("gopeed.plugins.settings.preview", handler("gopeed.plugins.settings.preview")),
		api.Handle("gopeed.settings.get.preview", handler("gopeed.settings.get.preview")),
		api.Handle("gopeed.settings.set.preview", handler("gopeed.settings.set.preview")),
		api.Handle("gopeed.publish.preview", handler("gopeed.publish.preview")),
		api.Handle("search.definitions.rollback.preview", handler("search.definitions.rollback.preview")),
		api.Handle("search.jobs.cancel.preview", handler("search.jobs.cancel.preview")),
	}
}
