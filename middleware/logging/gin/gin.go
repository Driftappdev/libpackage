package logging

import (
	"time"

	corelog "github.com/driftappdev/libpackage/core/logger"
	"github.com/gin-gonic/gin"
)

func Gin(log corelog.Logger) gin.HandlerFunc {
	if log == nil {
		log = corelog.New(corelog.Options{})
	}
	return func(c *gin.Context) {
		start := time.Now()
		c.Next()
		log.InfoContext(c.Request.Context(), "http_request",
			corelog.String("method", c.Request.Method),
			corelog.String("path", c.FullPath()),
			corelog.Int("status", c.Writer.Status()),
			corelog.Int64("duration_ms", time.Since(start).Milliseconds()),
		)
	}
}
