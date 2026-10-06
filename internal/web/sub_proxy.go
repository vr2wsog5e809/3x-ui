package web

import (
	"net/http"
	"net/http/httputil"
	"net/url"
	"strconv"
	"strings"

	"github.com/mhsanaei/3x-ui/v3/internal/logger"

	"github.com/gin-gonic/gin"
)

// installSubscriptionReverseProxy exposes the subscription server through the
// same public HTTP listener as the panel. This is useful on platforms such as
// Railway where only the web listener is publicly exposed: /sub/, /json/ and
// /clash/ are forwarded to the local subscription server instead of requiring
// the subscription port to be published separately.
func (s *Server) installSubscriptionReverseProxy(engine *gin.Engine) error {
	enabled, err := s.settingService.GetSubEnable()
	if err != nil {
		return err
	}
	if !enabled {
		return nil
	}

	port, err := s.settingService.GetSubPort()
	if err != nil {
		return err
	}

	rawPaths := make([]string, 0, 3)
	if p, err := s.settingService.GetSubPath(); err == nil {
		rawPaths = append(rawPaths, p)
	} else {
		return err
	}
	if p, err := s.settingService.GetSubJsonPath(); err == nil {
		rawPaths = append(rawPaths, p)
	} else {
		return err
	}
	if p, err := s.settingService.GetSubClashPath(); err == nil {
		rawPaths = append(rawPaths, p)
	} else {
		return err
	}

	prefixes := normalizeSubscriptionProxyPrefixes(rawPaths)
	if len(prefixes) == 0 {
		return nil
	}

	target := &url.URL{
		Scheme: "http",
		Host:   "127.0.0.1:" + strconv.Itoa(port),
	}
	proxy := httputil.NewSingleHostReverseProxy(target)
	proxy.ErrorHandler = func(w http.ResponseWriter, r *http.Request, err error) {
		logger.Warning("Subscription reverse proxy error: ", err)
		http.Error(w, "subscription service unavailable", http.StatusBadGateway)
	}

	originalDirector := proxy.Director
	proxy.Director = func(req *http.Request) {
		originalHost := req.Host
		scheme := "http"
		if req.TLS != nil {
			scheme = "https"
		}

		originalDirector(req)
		// Keep the public host visible to the subscription server. This matters
		// for domain validation and for handlers/templates that inspect Host.
		req.Host = originalHost
		req.Header.Set("X-Forwarded-Host", originalHost)
		req.Header.Set("X-Forwarded-Proto", scheme)
	}

	logger.Info("Subscription reverse proxy enabled on", strings.Join(prefixes, ", "), "-> 127.0.0.1:", port)

	engine.Use(func(c *gin.Context) {
		if hasSubscriptionProxyPrefix(c.Request.URL.Path, prefixes) {
			proxy.ServeHTTP(c.Writer, c.Request)
			c.Abort()
			return
		}
		c.Next()
	})

	return nil
}

func normalizeSubscriptionProxyPrefixes(paths []string) []string {
	out := make([]string, 0, len(paths))
	seen := make(map[string]struct{}, len(paths))
	for _, path := range paths {
		path = strings.TrimSpace(path)
		if path == "" || path == "/" {
			continue
		}
		if !strings.HasPrefix(path, "/") {
			path = "/" + path
		}
		path = "/" + strings.Trim(path, "/")
		if path == "/" {
			continue
		}
		if _, ok := seen[path]; ok {
			continue
		}
		seen[path] = struct{}{}
		out = append(out, path)
	}
	return out
}

func hasSubscriptionProxyPrefix(requestPath string, prefixes []string) bool {
	for _, prefix := range prefixes {
		if requestPath == prefix || strings.HasPrefix(requestPath, prefix+"/") {
			return true
		}
	}
	return false
}
