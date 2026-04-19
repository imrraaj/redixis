package httpapi

import (
	"net/http"

	"github.com/gin-gonic/gin"
)

type errorResponse struct {
	Error   string `json:"error"`
	Message string `json:"message"`
}

func writeError(c *gin.Context, status int, code string, message string) {
	c.AbortWithStatusJSON(status, errorResponse{
		Error:   code,
		Message: message,
	})
}

func writeBadRequest(c *gin.Context, message string) {
	writeError(c, http.StatusBadRequest, "bad_request", message)
}
