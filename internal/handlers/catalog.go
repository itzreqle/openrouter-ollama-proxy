package handlers

import (
	"net/http"
)

// CatalogDebug handles GET /orollama/catalog — ranked OpenRouter entries + resolved roles.
func (a *API) CatalogDebug(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}
	a.EnsureCatalog(r.Context())
	if a.Catalog == nil {
		writeError(w, http.StatusServiceUnavailable, "catalog unavailable")
		return
	}
	res := a.Catalog.Resolved()
	writeJSON(w, http.StatusOK, map[string]any{
		"sort":      a.Cfg.OpenRouterSort,
		"free_only": a.Cfg.OpenRouterFreeOnly,
		"tools_only": a.Cfg.OpenRouterToolsOnly,
		"resolved": map[string]any{
			"opus":        res.Opus,
			"sonnet":      res.Sonnet,
			"haiku":       res.Haiku,
			"default":     res.Default,
			"opus_auto":   res.OpusAuto,
			"sonnet_auto": res.SonnetAuto,
			"haiku_auto":  res.HaikuAuto,
			"sonnet_ctx":  res.SonnetCtx,
			"opus_ctx":    res.OpusCtx,
			"haiku_ctx":   res.HaikuCtx,
		},
		"models": a.Catalog.Entries(),
	})
}
