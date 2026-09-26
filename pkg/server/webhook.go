package server

import (
	"cmp"
	"net/http"
	"strings"

	json "github.com/bytedance/sonic"
)

// handleTautulli handles webhooks from Tautulli. The payload must carry a
// tvdb/tmdb id (or a generic media_id): the repair system then runs a targeted
// recheck against that specific media — the v2 equivalent of v1's
// "media-id-scoped repair job".
//
// A payload with no media id is rejected with 400. It used to fall through to
// a full library sweep with the operator's configured repair settings, so a
// single untargeted notification could mass-delete. The check runs before the
// repair service is looked up, so no path remains from an untargeted payload
// to a sweep.
func (s *Server) handleTautulli(w http.ResponseWriter, r *http.Request) {
	var payload struct {
		Topic   string `json:"topic"`
		Arr     string `json:"arr,omitempty"`
		MediaID string `json:"media_id,omitempty"`
		TvdbID  string `json:"tvdb_id,omitempty"`
		TmdbID  string `json:"tmdb_id,omitempty"`
		Fix     bool   `json:"fix,omitempty"`
	}
	if err := json.ConfigDefault.NewDecoder(r.Body).Decode(&payload); err != nil {
		s.logger.Error().Err(err).Msg("Failed to parse webhook body")
		http.Error(w, "Failed to parse webhook body: "+err.Error(), http.StatusBadRequest)
		return
	}
	if payload.Topic != "tautulli" {
		http.Error(w, "Invalid topic", http.StatusBadRequest)
		return
	}

	mediaID := strings.TrimSpace(cmp.Or(payload.MediaID, payload.TmdbID, payload.TvdbID))
	if mediaID == "" {
		// No targeting. Never fall back to a full sweep from a webhook.
		http.Error(w, "media_id (or tmdb_id / tvdb_id) is required", http.StatusBadRequest)
		return
	}

	svc := s.manager.Repair()
	if svc == nil {
		http.Error(w, "Repair service not available", http.StatusServiceUnavailable)
		return
	}

	run, err := svc.RecheckMedia(s.manager.Context(), strings.TrimSpace(payload.Arr), mediaID, payload.Fix)
	if err != nil {
		status := http.StatusBadRequest
		if strings.Contains(err.Error(), "already running") {
			status = http.StatusConflict
		}
		http.Error(w, err.Error(), status)
		return
	}

	if run != nil {
		s.logger.Info().
			Str("run_id", run.ID).
			Str("arr", payload.Arr).
			Str("media_id", mediaID).
			Bool("fix", payload.Fix).
			Msg("Tautulli webhook: media recheck triggered")
	}
	w.WriteHeader(http.StatusOK)
}
