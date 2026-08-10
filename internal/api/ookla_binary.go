package api

import (
	"context"
	"errors"
	"net/http"
	"time"

	ooklaprovider "github.com/dude2k/MultiSpeed/internal/providers/ookla"
)

func (s *Server) getOoklaBinaryStatus(w http.ResponseWriter, r *http.Request) {
	if s.ooklaBinary == nil {
		writeJSON(w, http.StatusOK, ooklaprovider.BinaryStatus{MaxUploadBytes: ooklaprovider.MaxBinaryUploadBytes, Message: "Manual Ookla executable upload is disabled by this deployment."})
		return
	}
	writeJSON(w, http.StatusOK, s.ooklaBinary.Status())
}

func (s *Server) uploadOoklaBinary(w http.ResponseWriter, r *http.Request) {
	if s.ooklaUploadLimit != nil && !s.ooklaUploadLimit.Allow(r) {
		writeError(w, r, http.StatusTooManyRequests, "RATE_LIMITED", "Ookla executable upload is rate limited.")
		return
	}
	if s.ooklaBinary == nil || !s.ooklaBinary.Status().UploadEnabled {
		writeError(w, r, http.StatusForbidden, "OOKLA_UPLOAD_DISABLED", "Manual Ookla executable upload is disabled by this deployment.")
		return
	}
	settings, err := s.store.GetSettings(r.Context())
	if err != nil {
		handleStoreError(w, r, err)
		return
	}
	if !s.withEffectiveOoklaEULA(settings).OoklaEULAEffectiveAccepted {
		writeError(w, r, http.StatusUnprocessableEntity, "OOKLA_EULA_REQUIRED", "Review and accept the current Ookla EULA before uploading an executable.")
		return
	}
	if r.ContentLength == 0 {
		writeError(w, r, http.StatusUnprocessableEntity, "INVALID_OOKLA_BINARY", "Select a Linux amd64 Speedtest by Ookla executable to upload.")
		return
	}
	if r.ContentLength > ooklaprovider.MaxBinaryUploadBytes {
		writeError(w, r, http.StatusRequestEntityTooLarge, "OOKLA_BINARY_TOO_LARGE", "The Ookla executable must not exceed 64 MiB.")
		return
	}
	r.Body = http.MaxBytesReader(w, r.Body, ooklaprovider.MaxBinaryUploadBytes+1)
	installContext, cancel := context.WithTimeout(r.Context(), 20*time.Second)
	defer cancel()
	result, err := s.ooklaBinary.Install(installContext, r.Body)
	if err != nil {
		switch {
		case errors.Is(err, ooklaprovider.ErrBinaryUploadDisabled):
			writeError(w, r, http.StatusForbidden, "OOKLA_UPLOAD_DISABLED", "Manual Ookla executable upload is disabled by this deployment.")
		case errors.Is(err, ooklaprovider.ErrBinaryTooLarge):
			writeError(w, r, http.StatusRequestEntityTooLarge, "OOKLA_BINARY_TOO_LARGE", "The Ookla executable must not exceed 64 MiB.")
		case errors.Is(err, ooklaprovider.ErrInvalidBinary):
			writeError(w, r, http.StatusUnprocessableEntity, "INVALID_OOKLA_BINARY", "The file must be a Linux amd64 Speedtest by Ookla executable with valid --version output.")
		default:
			s.logger.Error("install Ookla executable", "request_id", requestIDFrom(r.Context()), "error", err)
			writeError(w, r, http.StatusInternalServerError, "OOKLA_INSTALL_FAILED", "The Ookla executable could not be installed.")
		}
		return
	}
	if s.broker != nil {
		s.broker.Publish("provider.updated", map[string]string{"provider": "ookla"})
	}
	writeJSON(w, http.StatusCreated, result)
}
