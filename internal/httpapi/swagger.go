package httpapi

import (
	_ "embed"
	"net/http"

	"github.com/gin-gonic/gin"
)

//go:embed markup/openapi.yaml
var openAPISpec []byte

//go:embed markup/swagger-ui.html
var swaggerUIHTML []byte

func openAPIHandler(c *gin.Context) {
	c.Data(http.StatusOK, "application/yaml; charset=utf-8", openAPISpec)
}

func swaggerUIHandler(c *gin.Context) {
	c.Data(http.StatusOK, "text/html; charset=utf-8", swaggerUIHTML)
}
