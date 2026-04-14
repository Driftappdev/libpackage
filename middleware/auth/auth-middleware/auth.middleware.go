package auth

import "net/http"

// HTTPValidator validates an incoming HTTP request before passing it downstream.
type HTTPValidator func(r *http.Request) error

// HTTP provides a standard net/http auth middleware.
func HTTP(validate HTTPValidator) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if validate == nil {
				next.ServeHTTP(w, r)
				return
			}
			if err := validate(r); err != nil {
				http.Error(w, err.Error(), http.StatusUnauthorized)
				return
			}
			next.ServeHTTP(w, r)
		})
	}
}
