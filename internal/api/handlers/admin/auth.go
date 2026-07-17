package admin

import (
	"encoding/json"
	"net/http"
	"time"

	"go.uber.org/zap"
	"gorm.io/gorm"

	"github.com/jigar225/goget/internal/core/auth"
)

type AuthHandler struct {
	baseHandler
	masterKeyService *auth.MasterKeyService
	authService      *auth.AuthService
	db               *gorm.DB
}

func NewAuthHandler(logger *zap.Logger, masterKeyService *auth.MasterKeyService, authService *auth.AuthService, db *gorm.DB) *AuthHandler {
	return &AuthHandler{
		baseHandler:      baseHandler{logger: logger},
		masterKeyService: masterKeyService,
		authService:      authService,
		db:               db,
	}
}

type LoginRequest struct {
	MasterKey string `json:"master_key"`
}

type LoginResponse struct {
	Success bool   `json:"success"`
	Token   string `json:"token"`
	Message string `json:"message"`
}

// MasterKeyLogin handles master key authentication for admin access.
func (h *AuthHandler) MasterKeyLogin(w http.ResponseWriter, r *http.Request) {
	var req LoginRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		h.sendError(w, http.StatusBadRequest, "Invalid request body")
		return
	}

	if req.MasterKey == "" {
		h.sendError(w, http.StatusBadRequest, "Master key is required")
		return
	}

	masterCtx, err := h.masterKeyService.ValidateMasterKey(r.Context(), req.MasterKey)
	if err != nil {
		h.sendError(w, http.StatusUnauthorized, "Invalid master key")
		return
	}

	token, err := h.masterKeyService.GenerateAdminToken(masterCtx)
	if err != nil {
		h.sendError(w, http.StatusInternalServerError, "Failed to generate token")
		return
	}

	h.sendJSON(w, http.StatusOK, LoginResponse{
		Success: true,
		Token:   token,
		Message: "Master key authentication successful",
	})
}

// Login accepts master key login for backward compatibility.
func (h *AuthHandler) Login(w http.ResponseWriter, r *http.Request) {
	h.MasterKeyLogin(w, r)
}

// Validate checks if a master key or admin session token is valid.
func (h *AuthHandler) Validate(w http.ResponseWriter, r *http.Request) {
	authHeader := r.Header.Get("Authorization")
	if authHeader == "" {
		h.sendError(w, http.StatusUnauthorized, "Authorization header required")
		return
	}

	token := authHeader
	if len(authHeader) > 7 && authHeader[:7] == "Bearer " {
		token = authHeader[7:]
	}

	if masterCtx, err := h.masterKeyService.ValidateMasterKey(r.Context(), token); err == nil {
		h.sendJSON(w, http.StatusOK, map[string]interface{}{
			"valid": true,
			"user": map[string]interface{}{
				"id":     "master",
				"role":   "admin",
				"type":   "master_key",
				"scopes": masterCtx.Scopes,
			},
			"expires_at": masterCtx.ValidatedAt.Add(24 * time.Hour).Unix(),
		})
		return
	}

	if _, err := h.masterKeyService.ValidateAdminToken(token); err == nil {
		h.sendJSON(w, http.StatusOK, map[string]interface{}{
			"valid": true,
			"user": map[string]interface{}{
				"id":   "master",
				"role": "admin",
				"type": "admin_token",
			},
			"expires_at": time.Now().Add(24 * time.Hour).Unix(),
		})
		return
	}

	h.sendError(w, http.StatusUnauthorized, "Invalid token")
}
