package ratelimit

import (
	"errors"
	"net/http"
	"strconv"

	core "github.com/driftappdev/libpackage/ratelimit"
	"github.com/gin-gonic/gin"
)

func Gin(l *core.Limiter, extract KeyExtractor) gin.HandlerFunc {
	if extract == nil {
		extract = ByRemoteAddr
	}
	return func(c *gin.Context) {
		if l == nil {
			c.Next()
			return
		}
		res, err := l.Allow(c.Request.Context(), core.Key{Namespace: "http", Identity: extract(c.Request)})
		if err == nil || errors.Is(err, core.ErrLimited) {
			c.Header(HeaderLimit, strconv.FormatInt(res.Limit, 10))
			c.Header(HeaderRemaining, strconv.FormatInt(res.Remaining, 10))
			c.Header(HeaderReset, strconv.FormatInt(res.ResetAt.Unix(), 10))
		}
		if errors.Is(err, core.ErrLimited) {
			c.AbortWithStatusJSON(http.StatusTooManyRequests, gin.H{"error": "rate limited"})
			return
		}
		if err != nil {
			c.AbortWithStatusJSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
			return
		}
		c.Next()
	}
}
