package middleware

import (
	"fmt"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/insmtx/Lework/backend/internal/api/auth"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/ygpkg/yg-go/apis/constants"
	"github.com/ygpkg/yg-go/apis/runtime"
	"github.com/ygpkg/yg-go/logs"
	"github.com/ygpkg/yg-go/metrics"
)

// Logger .
func Logger(whitelist ...string) gin.HandlerFunc {
	return func(ctx *gin.Context) {
		caller, _ := auth.FromContext(ctx)

		uin := uint(0)
		if caller != nil {
			uin = caller.Uin
		}

		logs.SetContextFields(ctx, constants.CtxKeyUin, uin)
		currReq := ctx.FullPath()
		for _, whitelistItem := range whitelist {
			if strings.HasSuffix(currReq, whitelistItem) {
				ctx.Next()
				return
			}
		}

		start := time.Now()
		ctx.Next()
		cost := time.Since(start)
		metrics.Histogram("request_latency_seconds").
			With(prometheus.Labels{
				"uri":  ctx.Request.URL.Path,
				"code": fmt.Sprint(ctx.Writer.Status()),
			}).Buckets(0.2, 0.8, 1.6, 5, 10, 60).
			Observe(cost.Seconds())
		if ctx.Writer.Status() >= 500 {
			logs.LoggerFromContext(ctx).Errorw(fmt.Sprint(ctx.Writer.Status()),
				"method", ctx.Request.Method,
				"uri", ctx.Request.RequestURI,
				"reqsize", ctx.Request.ContentLength,
				"latency", fmt.Sprintf("%.3f", cost.Seconds()),
				"clientip", runtime.GetRealIP(ctx.Request),
				"respsize", ctx.Writer.Size(),
				"referer", ctx.Request.Referer(),
			)
		} else {
			code := ctx.GetInt(constants.CtxKeyCode)
			logs.LoggerFromContext(ctx).Infow(fmt.Sprint(code),
				"method", ctx.Request.Method,
				"uri", ctx.Request.RequestURI,
				"reqsize", ctx.Request.ContentLength,
				"latency", fmt.Sprintf("%.3f", cost.Seconds()),
				"clientip", runtime.GetRealIP(ctx.Request),
				"respsize", ctx.Writer.Size(),
				"referer", ctx.Request.Referer(),
			)
		}
	}
}
