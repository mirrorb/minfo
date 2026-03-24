package middleware

import (
	"crypto/subtle"
	"log"
	"net/http"
	"time"

	"minfo/internal/config"
	"minfo/internal/httpapi/transport"
)

func Logging(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		next.ServeHTTP(w, r)
		log.Printf("%s %s %s", r.Method, r.URL.Path, time.Since(start))
	})
}

func Authenticate(next http.Handler) http.Handler {
	username := config.Getenv("WEB_USERNAME", "")
	password := config.Getenv("WEB_PASSWORD", "")
	if password == "" {
		return next
	}

	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		user, pass, ok := r.BasicAuth()
		if !ok || !matchesCredential(password, pass) || (username != "" && !matchesCredential(username, user)) {
			w.Header().Set("WWW-Authenticate", "Basic realm=\"minfo\"")
			transport.WriteError(w, http.StatusUnauthorized, "unauthorized")
			return
		}
		next.ServeHTTP(w, r)
	})
}

func matchesCredential(expected, actual string) bool {
	return subtle.ConstantTimeCompare([]byte(expected), []byte(actual)) == 1
}
