package adminshield

import (
	"context"
	"errors"
	"net/http"
	"os"
	"strings"

	goauth "github.com/driftappdev/libpackage/auth"
	"github.com/gin-gonic/gin"
)

const (
	ctxClaimsKey = "admin_claims"
)

var (
	ErrMissingSecret = errors.New("adminshield: missing ADMIN_JWT_SECRET/JWT_SECRET")
)

type Config struct {
	SecretEnvPrimary  string
	SecretEnvFallback string
	BypassEnv         string
}

func DefaultConfig() Config {
	return Config{
		SecretEnvPrimary:  "ADMIN_JWT_SECRET",
		SecretEnvFallback: "JWT_SECRET",
		BypassEnv:         "ADMIN_AUTH_BYPASS",
	}
}

func secretFromEnv(cfg Config) string {
	if cfg.SecretEnvPrimary == "" {
		cfg.SecretEnvPrimary = "ADMIN_JWT_SECRET"
	}
	if cfg.SecretEnvFallback == "" {
		cfg.SecretEnvFallback = "JWT_SECRET"
	}
	secret := strings.TrimSpace(os.Getenv(cfg.SecretEnvPrimary))
	if secret != "" {
		return secret
	}
	return strings.TrimSpace(os.Getenv(cfg.SecretEnvFallback))
}

func bypass(cfg Config) bool {
	if cfg.BypassEnv == "" {
		cfg.BypassEnv = "ADMIN_AUTH_BYPASS"
	}
	v := strings.ToLower(strings.TrimSpace(os.Getenv(cfg.BypassEnv)))
	return v == "1" || v == "true" || v == "yes"
}

func authManager(cfg Config) (*goauth.Manager, error) {
	secret := secretFromEnv(cfg)
	if secret == "" {
		return nil, ErrMissingSecret
	}
	signer := goauth.NewHMACSigner([]byte(secret))
	return goauth.NewManager(goauth.Config{
		Signer:      signer,
		Verifier:    signer,
		TokenLookup: "header:Authorization",
	}), nil
}

func parseBearer(h string) string {
	parts := strings.SplitN(strings.TrimSpace(h), " ", 2)
	if len(parts) != 2 || !strings.EqualFold(parts[0], "Bearer") {
		return ""
	}
	return strings.TrimSpace(parts[1])
}

func claimsFromRequest(r *http.Request, cfg Config) (*goauth.Claims, int, error) {
	if bypass(cfg) {
		return &goauth.Claims{Subject: "admin-bypass", Roles: []string{"SUPER_ADMIN"}}, http.StatusOK, nil
	}
	manager, err := authManager(cfg)
	if err != nil {
		return nil, http.StatusInternalServerError, err
	}
	token := parseBearer(r.Header.Get("Authorization"))
	if token == "" {
		return nil, http.StatusUnauthorized, errors.New("missing bearer token")
	}
	claims, err := manager.Parse(token)
	if err != nil {
		return nil, http.StatusUnauthorized, err
	}
	return claims, http.StatusOK, nil
}

func allowed(claims *goauth.Claims, roles []string) bool {
	if len(roles) == 0 {
		return true
	}
	return claims.HasRole(roles...)
}

func writeError(w http.ResponseWriter, status int, msg string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_, _ = w.Write([]byte(`{"error":"` + msg + `"}`))
}

func ClaimsFromContext(ctx context.Context) (*goauth.Claims, bool) {
	return goauth.FromContext(ctx)
}

func GinRequireRoles(roles ...string) gin.HandlerFunc {
	cfg := DefaultConfig()
	return func(c *gin.Context) {
		claims, code, err := claimsFromRequest(c.Request, cfg)
		if err != nil {
			c.AbortWithStatusJSON(code, gin.H{"error": err.Error()})
			return
		}
		if !allowed(claims, roles) {
			c.AbortWithStatusJSON(http.StatusForbidden, gin.H{"error": "forbidden"})
			return
		}
		c.Set(ctxClaimsKey, claims)
		c.Request = c.Request.WithContext(contextWithClaims(c.Request.Context(), claims))
		c.Next()
	}
}

func ChiRequireRoles(roles ...string) func(http.Handler) http.Handler {
	cfg := DefaultConfig()
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			claims, code, err := claimsFromRequest(r, cfg)
			if err != nil {
				writeError(w, code, err.Error())
				return
			}
			if !allowed(claims, roles) {
				writeError(w, http.StatusForbidden, "forbidden")
				return
			}
			next.ServeHTTP(w, r.WithContext(contextWithClaims(r.Context(), claims)))
		})
	}
}

func contextWithClaims(ctx context.Context, claims *goauth.Claims) context.Context {
	return context.WithValue(ctx, claimsContextKey{}, claims)
}

type claimsContextKey struct{}

func ClaimsFromHTTPContext(ctx context.Context) (*goauth.Claims, bool) {
	claims, ok := ctx.Value(claimsContextKey{}).(*goauth.Claims)
	return claims, ok
}
