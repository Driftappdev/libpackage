package binding

import (
	"net/http"

	"github.com/gin-gonic/gin"
)

func BindJSON[T any](c *gin.Context) (T, error) {
	var out T
	err := c.ShouldBindJSON(&out)
	return out, err
}

func BindQuery[T any](r *http.Request, target *T) error {
	_, _ = r, target
	return nil
}
