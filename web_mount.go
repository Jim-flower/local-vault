package main

import (
	"context"
	"fmt"
	"net/http"
	"os"
	"strings"
)

// Each listener has its own mount and access policy. The maintenance listener
// stays at / even when the public listener is mounted below a gateway path.
type webOptions struct {
	BasePath    string
	AllowRemote bool
	LocalOnly   bool
}

type basePathContextKey struct{}

func normalizeBasePath(value string) (string, error) {
	value = strings.TrimSpace(value)
	if value == "" || value == "/" {
		return "/", nil
	}
	value = strings.Trim(value, "/")
	for _, segment := range strings.Split(value, "/") {
		if segment == "" || segment == "." || segment == ".." {
			return "", fmt.Errorf("invalid base path: empty or relative segment")
		}
		for _, char := range segment {
			if !(char >= 'a' && char <= 'z' || char >= 'A' && char <= 'Z' || char >= '0' && char <= '9' || strings.ContainsRune("-_~.", char)) {
				return "", fmt.Errorf("invalid base path: use URL path segments containing letters, numbers, -, _, ~ or .")
			}
		}
	}
	return "/" + value + "/", nil
}

func requestBasePath(request *http.Request) string {
	if path, ok := request.Context().Value(basePathContextKey{}).(string); ok {
		return path
	}
	return "/"
}

func webAccessAllowed(request *http.Request, options webOptions) bool {
	if options.LocalOnly {
		return isLoopbackRequest(request) || os.Getenv("DEVHUB_ALLOW_INSECURE_LOCAL") == "1" && isLoopbackHost(request.Host)
	}
	if isLoopbackRequest(request) && isLoopbackHost(request.Host) {
		return true
	}
	return options.AllowRemote && (os.Getenv("DEVHUB_REQUIRE_HTTPS") == "0" || isSecureWebRequest(request))
}

func mountWebRouter(handler http.Handler, options webOptions) http.Handler {
	basePath, err := normalizeBasePath(options.BasePath)
	if err != nil {
		panic(err) // Application configuration is validated before starting servers.
	}
	options.BasePath = basePath
	prefix := strings.TrimSuffix(basePath, "/")
	mounted := handler
	if prefix != "" {
		mounted = http.StripPrefix(prefix, handler)
	}
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Only the public listener explicitly enabled for proxy access trusts
		// forwarded headers. Its network exposure is restricted by deployment.
		if options.LocalOnly || !options.AllowRemote {
			r = r.Clone(r.Context())
			r.Header.Del("X-Forwarded-Proto")
			r.Header.Del("X-Forwarded-For")
		}
		if !webAccessAllowed(r, options) {
			writeWebJSON(w, http.StatusForbidden, webResponse{Error: "remote access requires an enabled HTTPS gateway"})
			return
		}
		if prefix != "" && r.URL.Path == prefix {
			target := basePath
			if r.URL.RawQuery != "" {
				target += "?" + r.URL.RawQuery
			}
			http.Redirect(w, r, target, http.StatusPermanentRedirect)
			return
		}
		if !strings.HasPrefix(r.URL.Path, basePath) {
			http.NotFound(w, r)
			return
		}
		r = r.WithContext(context.WithValue(r.Context(), basePathContextKey{}, basePath))
		mounted.ServeHTTP(w, r)
	})
}
