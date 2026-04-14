package cors

import (
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"
)

type Options struct {
	AllowOrigins []string
	AllowMethods []string
	AllowHeaders []string
}

func Gin(opts Options) gin.HandlerFunc {
	allowOrigins := strings.Join(opts.AllowOrigins, ", ")
	allowMethods := strings.Join(opts.AllowMethods, ", ")
	allowHeaders := strings.Join(opts.AllowHeaders, ", ")
	return func(c *gin.Context) {
		if allowOrigins != "" {
			c.Writer.Header().Set("Access-Control-Allow-Origin", allowOrigins)
		}
		if allowMethods != "" {
			c.Writer.Header().Set("Access-Control-Allow-Methods", allowMethods)
		}
		if allowHeaders != "" {
			c.Writer.Header().Set("Access-Control-Allow-Headers", allowHeaders)
		}
		if c.Request.Method == http.MethodOptions {
			c.AbortWithStatus(http.StatusNoContent)
			return
		}
		c.Next()
	}
}
