package handlers

import (
	"net/http"
	"path"

	"github.com/Bengo-Hub/httpware/objectstore"
	authclient "github.com/Bengo-Hub/shared-auth-client"
	"go.uber.org/zap"
)

// MediaHandler handles media uploads (currently: Patient.photo_url capture at registration) on the
// shared object storage (httpware/objectstore). Patient photos are private: stored under
// hospital/<tenant>/patient-photos/..., handed out only as short-lived signed links.
type MediaHandler struct {
	log   *zap.Logger
	store *objectstore.Store
}

// NewMediaHandler creates the upload handler.
func NewMediaHandler(log *zap.Logger, store *objectstore.Store) *MediaHandler {
	return &MediaHandler{log: log.Named("media.handler"), store: store}
}

// Upload handles POST /api/v1/media/upload (multipart form, field "file"): one JPEG or PNG patient
// photo up to 5 MB, checked from the bytes and re-encoded (EXIF removed). It answers a signed link
// so the registration form can preview it; patient create/update stores the key.
func (h *MediaHandler) Upload(w http.ResponseWriter, r *http.Request) {
	claims, ok := authclient.ClaimsFromContext(r.Context())
	if !ok || claims.TenantID == "" {
		respondError(w, http.StatusUnauthorized, "sign in to an organisation to upload")
		return
	}
	if !h.store.Enabled() {
		respondError(w, http.StatusServiceUnavailable, "file storage is not configured")
		return
	}
	up, err := objectstore.ReadUpload(w, r, "file", objectstore.TypePatientPhotos)
	if err != nil {
		if objectstore.IsUploadError(err) {
			respondError(w, http.StatusBadRequest, err.Error())
			return
		}
		respondError(w, http.StatusInternalServerError, "failed to read the upload")
		return
	}
	key, err := h.store.NewKey(objectstore.Owner{TenantID: claims.TenantID}, objectstore.TypePatientPhotos, up.Ext)
	if err != nil {
		respondError(w, http.StatusBadRequest, "invalid organisation")
		return
	}
	if err := h.store.Put(r.Context(), key, up.Data, up.ContentType); err != nil {
		h.log.Error("patient photo not stored", zap.Error(err))
		respondError(w, http.StatusBadGateway, "file storage is unavailable")
		return
	}
	respondJSON(w, http.StatusCreated, map[string]string{"url": h.store.URL(key), "filename": path.Base(key)})
}
