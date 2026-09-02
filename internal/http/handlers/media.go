package handlers

import (
	"fmt"
	"image"
	"image/jpeg"
	"image/png"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/google/uuid"
	"go.uber.org/zap"

	"github.com/bengobox/hospital-service/internal/config"
)

// MediaHandler handles media file uploads (currently: Patient.photo_url capture at
// registration). Mirrors inventory-api's own internal/http/handlers/media.go byte-for-byte in
// approach — same size cap, same JPEG/PNG-only allowlist, same EXIF-stripping re-encode — so a
// future engineer touching either doesn't need to relearn the pattern from scratch.
type MediaHandler struct {
	log *zap.Logger
	cfg config.MediaConfig
}

// NewMediaHandler creates a new media handler.
func NewMediaHandler(log *zap.Logger, cfg config.MediaConfig) *MediaHandler {
	return &MediaHandler{log: log.Named("media.handler"), cfg: cfg}
}

// Upload handles POST /api/v1/media/upload (multipart form, field "file"). Validates size/content
// type, strips EXIF metadata via re-encoding, saves under {MEDIA_ROOT}/uploads/patients/.
func (h *MediaHandler) Upload(w http.ResponseWriter, r *http.Request) {
	r.Body = http.MaxBytesReader(w, r.Body, 2*1024*1024)
	if err := r.ParseMultipartForm(2 * 1024 * 1024); err != nil {
		h.log.Error("failed to parse multipart form", zap.Error(err))
		respondError(w, http.StatusBadRequest, "file too large (max 2MB)")
		return
	}

	file, header, err := r.FormFile("file")
	if err != nil {
		h.log.Error("failed to get file from form", zap.Error(err))
		respondError(w, http.StatusBadRequest, "invalid file upload")
		return
	}
	defer file.Close()

	buffer := make([]byte, 512)
	n, _ := file.Read(buffer)
	contentType := http.DetectContentType(buffer[:n])
	if _, err := file.Seek(0, 0); err != nil {
		respondError(w, http.StatusInternalServerError, "internal server error")
		return
	}

	// Only JPEG/PNG — the re-encode below strips EXIF and neutralizes image-based exploits.
	allowedTypes := map[string]bool{"image/jpeg": true, "image/jpg": true, "image/png": true}
	if !allowedTypes[contentType] {
		ext := strings.ToLower(filepath.Ext(header.Filename))
		headerCT := header.Header.Get("Content-Type")
		switch {
		case ext == ".jpg" || ext == ".jpeg" || headerCT == "image/jpeg":
			contentType = "image/jpeg"
		case ext == ".png" || headerCT == "image/png":
			contentType = "image/png"
		}
	}
	if !allowedTypes[contentType] {
		h.log.Warn("rejected file upload", zap.String("detected_type", contentType), zap.String("filename", header.Filename))
		respondError(w, http.StatusBadRequest, "only JPEG and PNG images are allowed")
		return
	}

	ext := strings.ToLower(filepath.Ext(header.Filename))
	if ext != ".jpg" && ext != ".jpeg" && ext != ".png" {
		if contentType == "image/jpeg" {
			ext = ".jpg"
		} else {
			ext = ".png"
		}
	}
	filename := fmt.Sprintf("%s_%d%s", uuid.New().String(), time.Now().Unix(), ext)

	dir := filepath.Join(h.cfg.Root, "uploads", "patients")
	if err := os.MkdirAll(dir, 0755); err != nil {
		h.log.Error("failed to create upload directory", zap.Error(err))
		respondError(w, http.StatusInternalServerError, "internal server error")
		return
	}

	dstPath := filepath.Join(dir, filename)
	dst, err := os.Create(dstPath)
	if err != nil {
		h.log.Error("failed to create destination file", zap.Error(err))
		respondError(w, http.StatusInternalServerError, "internal server error")
		return
	}
	defer dst.Close()

	written := false
	if contentType == "image/jpeg" || contentType == "image/png" {
		img, _, decErr := image.Decode(file)
		if decErr == nil {
			var encErr error
			if contentType == "image/jpeg" {
				encErr = jpeg.Encode(dst, img, &jpeg.Options{Quality: 85})
			} else {
				encErr = png.Encode(dst, img)
			}
			if encErr == nil {
				written = true
			} else {
				h.log.Warn("re-encoding failed, falling back to direct copy", zap.Error(encErr))
				if _, err := file.Seek(0, 0); err != nil {
					respondError(w, http.StatusInternalServerError, "internal server error")
					return
				}
				if err := dst.Truncate(0); err != nil {
					respondError(w, http.StatusInternalServerError, "internal server error")
					return
				}
				if _, err := dst.Seek(0, 0); err != nil {
					respondError(w, http.StatusInternalServerError, "internal server error")
					return
				}
			}
		}
	}
	if !written {
		if _, err := io.Copy(dst, file); err != nil {
			h.log.Error("failed to copy file", zap.Error(err))
			respondError(w, http.StatusInternalServerError, "internal server error")
			return
		}
	}

	relativePath := fmt.Sprintf("/media/uploads/patients/%s", filename)
	url := relativePath
	if h.cfg.URLBase != "" {
		url = strings.TrimRight(h.cfg.URLBase, "/") + relativePath
	}
	respondJSON(w, http.StatusCreated, map[string]string{"url": url, "filename": filename})
}
