package httpapi

import (
	"context"
	"time"

	"github.com/gin-gonic/gin"
)

// redisContext returns a context derived from the request context, with an
// optional timeout. If timeout is zero or negative the context is only
// cancelled when the request itself is cancelled.
func redisContext(c *gin.Context, timeout time.Duration) (context.Context, context.CancelFunc) {
	if timeout <= 0 {
		return context.WithCancel(c.Request.Context())
	}
	return context.WithTimeout(c.Request.Context(), timeout)
}
