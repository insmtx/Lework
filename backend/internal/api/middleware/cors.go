package middleware

import (
	"time"

	"github.com/gin-contrib/cors"
	"github.com/gin-gonic/gin"
)

func CORS() gin.HandlerFunc {
	return cors.New(cors.Config{
		AllowOriginFunc: func(origin string) bool { return true },
		AllowMethods:    []string{"GET", "POST", "PUT", "DELETE", "PATCH"},
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
