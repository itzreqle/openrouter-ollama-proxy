package handlers

import (
	"net/http"
	"time"
)

// Limits handles GET /orollama/limits — OpenRouter key / free-quota snapshot.
func (a *API) Limits(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}
	if a.OpenRouter == nil {
		writeError(w, http.StatusServiceUnavailable, "openrouter client not configured")
		return
	}

	info, err := a.OpenRouter.GetKey(r.Context())
	if err != nil {
		a.Log.Warn("openrouter key lookup failed", "err", err)
		writeError(w, http.StatusBadGateway, err.Error())
		return
	}

	if info.FreeModelDailyRequests != nil && info.FreeModelDailyRequests.Remaining <= 5 {
		a.Log.Warn("openrouter free daily requests low",
			"remaining", info.FreeModelDailyRequests.Remaining,
			"limit", info.FreeModelDailyRequests.Limit,
			"used", info.FreeModelDailyRequests.Used,
		)
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"ok":         true,
		"fetched_at": info.FetchedAt.Format(time.RFC3339),
		"openrouter": info,
	})
}
