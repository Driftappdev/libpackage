package security

import (
	"net/http"
	"strconv"
	"time"
)

// HeadersConfig configures security hardening HTTP headers.
type HeadersConfig struct {
	ContentSecurityPolicy string
	HSTSMaxAge            time.Duration
	HSTSIncludeSubdomains bool
	HSTSPreload           bool
	FrameOptions          string
	ContentTypeOptions    string
	ReferrerPolicy        string
	PermissionsPolicy     string
	DisableHSTS           bool
}

// HTTPHeadersMiddleware injects OWASP-style HTTP security headers.
func HTTPHeadersMiddleware(opts ...HeadersConfig) func(http.Handler) http.Handler {
	cfg := HeadersConfig{
		HSTSMaxAge:            365 * 24 * time.Hour,
		HSTSIncludeSubdomains: true,
		FrameOptions:          "DENY",
		ContentTypeOptions:    "nosniff",
		ReferrerPolicy:        "strict-origin-when-cross-origin",
	}
	if len(opts) > 0 {
		o := opts[0]
		if o.ContentSecurityPolicy != "" {
			cfg.ContentSecurityPolicy = o.ContentSecurityPolicy
		}
		if o.HSTSMaxAge > 0 {
			cfg.HSTSMaxAge = o.HSTSMaxAge
		}
		if o.FrameOptions != "" {
			cfg.FrameOptions = o.FrameOptions
		}
		if o.ContentTypeOptions != "" {
			cfg.ContentTypeOptions = o.ContentTypeOptions
		}
		if o.ReferrerPolicy != "" {
			cfg.ReferrerPolicy = o.ReferrerPolicy
		}
		if o.PermissionsPolicy != "" {
			cfg.PermissionsPolicy = o.PermissionsPolicy
		}
		cfg.HSTSIncludeSubdomains = o.HSTSIncludeSubdomains
		cfg.HSTSPreload = o.HSTSPreload
		cfg.DisableHSTS = o.DisableHSTS
	}

	hsts := buildHSTSValue(cfg)
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			h := w.Header()
			if !cfg.DisableHSTS && hsts != "" {
				h.Set("Strict-Transport-Security", hsts)
			}
			if cfg.FrameOptions != "" {
				h.Set("X-Frame-Options", cfg.FrameOptions)
			}
			if cfg.ContentTypeOptions != "" {
				h.Set("X-Content-Type-Options", cfg.ContentTypeOptions)
			}
			if cfg.ReferrerPolicy != "" {
				h.Set("Referrer-Policy", cfg.ReferrerPolicy)
			}
			if cfg.ContentSecurityPolicy != "" {
				h.Set("Content-Security-Policy", cfg.ContentSecurityPolicy)
			}
			if cfg.PermissionsPolicy != "" {
				h.Set("Permissions-Policy", cfg.PermissionsPolicy)
			}
			h.Del("Server")
			h.Set("Cross-Origin-Opener-Policy", "same-origin")
			h.Set("Cross-Origin-Resource-Policy", "same-origin")
			h.Set("X-XSS-Protection", "0")
			next.ServeHTTP(w, r)
		})
	}
}

func buildHSTSValue(cfg HeadersConfig) string {
	if cfg.DisableHSTS {
		return ""
	}
	v := "max-age=" + strconv.Itoa(int(cfg.HSTSMaxAge.Seconds()))
	if cfg.HSTSIncludeSubdomains {
		v += "; includeSubDomains"
	}
	if cfg.HSTSPreload {
		v += "; preload"
	}
	return v
}
