package management

import (
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"
)

// StartOAuthV8 dispatches login using the provider query parameter.
func (h *Handler) StartOAuthV8(c *gin.Context) {
	switch strings.ToLower(strings.TrimSpace(c.Query("provider"))) {
	case "":
		c.JSON(http.StatusBadRequest, gin.H{"error": "provider is required"})
	case "claude":
		h.RequestAnthropicToken(c)
	case "codex":
		h.RequestCodexToken(c)
	case "xai":
		h.RequestXAIToken(c)
	default:
		if !h.ServePluginAuthURL(c) {
			c.JSON(http.StatusNotFound, gin.H{"error": "provider_not_found"})
		}
	}
}

// ImportOAuthV8 dispatches credential import using the provider query parameter.
// This fork builds without API-key import providers, so every provider is reported as unknown.
func (h *Handler) ImportOAuthV8(c *gin.Context) {
	c.JSON(http.StatusNotFound, gin.H{"error": "provider_not_found"})
}
