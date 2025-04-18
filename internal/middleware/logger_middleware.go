package middleware

import (
	"go-chat-room/pkg/logger"
	"net/http"
	"time"

	"github.com/gin-gonic/gin"
	"go.uber.org/zap"
)

// func GinZapLogger(logger *zap.Logger) gin.HandlerFunc {
//     return func(c *gin.Context) {
//         start := time.Now() // Start timer
//         path := c.Request.URL.Path
//         query := c.Request.URL.RawQuery

//         // Process request
//         c.Next()

//         // Log details after request is processed
//         end := time.Now()
//         latency := end.Sub(start)
//         statusCode := c.Writer.Status()
//         clientIP := c.ClientIP()
//         method := c.Request.Method
//         userAgent := c.Request.UserAgent()
//         errorMessage := c.Errors.ByType(gin.ErrorTypePrivate).String() // Get Gin error messages

//         fields := []zap.Field{
//             zap.Int("status", statusCode),
//             zap.String("method", method),
//             zap.String("path", path),
//             zap.String("ip", clientIP),
//             zap.Duration("latency", latency),
//             zap.String("user_agent", userAgent),
//         }
//         if query != "" {
//             fields = append(fields, zap.String("query", query))
//         }
//         if errorMessage != "" {
//             fields = append(fields, zap.String("error", errorMessage))
//         }

//         // Choose log level based on status code
//         switch {
//         case statusCode >= http.StatusInternalServerError:
//             logger.Error("Request", fields...)
//         case statusCode >= http.StatusBadRequest:
//             logger.Warn("Request", fields...)
//         default:
//             logger.Info("Request", fields...)
//         }
//     }
// }

func GinZapLogger() gin.HandlerFunc {
	log := logger.L
	return func(c *gin.Context) {
		start := time.Now()
		path := c.Request.URL.Path
		query := c.Request.URL.RawQuery

		// Process request
		c.Next()

		// Log details after request is processed
		end := time.Now()
		latency := end.Sub(start)
		statusCode := c.Writer.Status()
		clientIP := c.ClientIP()
		method := c.Request.Method
		userAgent := c.Request.UserAgent()
		errorMessage := c.Errors.ByType(gin.ErrorTypePrivate).String()

		fields := []zap.Field{
			zap.Int("status", statusCode),
			zap.String("method", method),
			zap.String("path", path),
			zap.String("ip", clientIP),
			zap.Duration("latency", latency),
			zap.String("user_agent", userAgent),
		}
		if query != "" {
			fields = append(fields, zap.String("query", query))
		}
		if errorMessage != "" {
			fields = append(fields, zap.String("error", errorMessage))
		}

		// Choose log level based on status code
		switch {
		case statusCode >= http.StatusInternalServerError:
			log.Error("Request", fields...)
		case statusCode >= http.StatusBadRequest:
			log.Warn("Request", fields...)
		default:
			log.Info("Request", fields...)
		}
	}
}
