package requestid

import (
	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
)

const Header = "X-Request-ID"

func Gin() gin.HandlerFunc {
	return func(c *gin.Context) {
		id := c.GetHeader(Header)
		if id == "" {
			id = uuid.NewString()
		}
		c.Writer.Header().Set(Header, id)
		c.Set("request_id", id)
		c.Next()
	}
}
