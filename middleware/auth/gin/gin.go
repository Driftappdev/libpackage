package auth

import "github.com/gin-gonic/gin"

type GinValidator func(c *gin.Context) error

func Gin(validate GinValidator) gin.HandlerFunc {
	return func(c *gin.Context) {
		if validate == nil {
			c.Next()
			return
		}
		if err := validate(c); err != nil {
			c.AbortWithStatusJSON(401, gin.H{"error": err.Error()})
			return
		}
		c.Next()
	}
}
