package tracing

import (
	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
)

func Gin() gin.HandlerFunc {
	return func(c *gin.Context) {
		if c.Request.Header.Get("traceparent") == "" {
			c.Request.Header.Set("traceparent", uuid.NewString())
		}
		c.Next()
	}
}
