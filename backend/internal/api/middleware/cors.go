package middleware

import (
	"net/url"
	"os"
	"strings"
	"time"

	"github.com/gin-contrib/cors"
	"github.com/gin-gonic/gin"
)

// CORS 只回显白名单来源。显式传参优先，其次读 LEWORK_CORS_ALLOWED_ORIGINS，
// 最后读 LEWORK_PUBLIC_URL。未配置时不允许任何跨域来源；同源请求不依赖 CORS 头。
//
// 不能继续使用 AllowOriginFunc 恒 true + AllowCredentials：这等于允许任意网站
// 携带用户 Cookie 调用管理 API。
func CORS(allowedOrigins ...string) gin.HandlerFunc {
	allowed := buildAllowedOrigins(allowedOrigins)
	return cors.New(cors.Config{
		AllowOriginFunc: func(origin string) bool {
			_, ok := allowed[normalizeOrigin(origin)]
			return ok
		},
		AllowMethods: []string{"GET", "POST", "PUT", "DELETE", "PATCH"},
		AllowHeaders: []string{
			"Accept",
			"Origin",
			"Accept-Encoding",
			"Accept-Language",
			"Access-Control-Request-Headers",
			"Access-Control-Request-Method",
			"Host",
			"Proxy-Connection",
			"Referer",
			"Sec-Fetch-Mode",
			"User-Agent",
			"Content-Type",
			"Env",
			"Authorization",
			"Upgrade",
			"Connection",
			"Cache-Control",
			"X-Request-ID",
			"X-Trace-ID",
			"X-Requested-With",
			"X-Lework-Client-App",
			"X-Lework-Client-Version",
			"X-Lework-Client-Platform",
			"X-Lework-Client-Arch",
		},
		ExposeHeaders:    []string{"Content-Length"},
		AllowCredentials: true,
		MaxAge:           12 * time.Hour,
	})
}

func buildAllowedOrigins(explicit []string) map[string]struct{} {
	raw := make([]string, 0, len(explicit)+2)
	raw = append(raw, explicit...)
	if env := strings.TrimSpace(os.Getenv("LEWORK_CORS_ALLOWED_ORIGINS")); env != "" {
		raw = append(raw, strings.Split(env, ",")...)
	}
	if publicURL := strings.TrimSpace(os.Getenv("LEWORK_PUBLIC_URL")); publicURL != "" {
		raw = append(raw, publicURL)
	}

	allowed := make(map[string]struct{}, len(raw))
	for _, item := range raw {
		if origin := normalizeOrigin(item); origin != "" {
			allowed[origin] = struct{}{}
		}
	}
	return allowed
}

func normalizeOrigin(raw string) string {
	raw = strings.TrimSpace(strings.TrimRight(raw, "/"))
	if raw == "" {
		return ""
	}
	u, err := url.Parse(raw)
	if err != nil || u.Scheme == "" || u.Host == "" {
		// 自定义协议（如桌面端的 app://）没有 host，保留原值做精确匹配。
		return strings.ToLower(raw)
	}

	scheme := strings.ToLower(u.Scheme)
	host := strings.ToLower(u.Host)
	switch {
	case scheme == "http" && strings.HasSuffix(host, ":80"):
		host = strings.TrimSuffix(host, ":80")
	case scheme == "https" && strings.HasSuffix(host, ":443"):
		host = strings.TrimSuffix(host, ":443")
	}
	return scheme + "://" + host
}
