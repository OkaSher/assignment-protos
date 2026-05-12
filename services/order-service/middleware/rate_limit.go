package middleware

import (
    "context"
    "net"
    "net/http"
    "strings"
    "time"

    "github.com/redis/go-redis/v9"
)

// RateLimitMiddleware returns middleware that limits requests per minute per identifier (IP)
func RateLimitMiddleware(rdb *redis.Client, perMinute int) func(http.Handler) http.Handler {
    return func(next http.Handler) http.Handler {
        return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
            id := clientID(r)
            key := "rl:" + id + ":" + time.Now().Format("200601021504")
            ctx := context.Background()
            v, err := rdb.Incr(ctx, key).Result()
            if err == nil && v == 1 {
                _ = rdb.Expire(ctx, key, time.Minute).Err()
            }
            if err != nil {
                // on redis error, allow through
                next.ServeHTTP(w, r)
                return
            }
            if v > int64(perMinute) {
                w.WriteHeader(http.StatusTooManyRequests)
                w.Write([]byte("rate limit exceeded"))
                return
            }
            next.ServeHTTP(w, r)
        })
    }
}

func clientID(r *http.Request) string {
    // prefer X-Real-IP or X-Forwarded-For
    if xr := r.Header.Get("X-Real-IP"); xr != "" {
        return xr
    }
    if xf := r.Header.Get("X-Forwarded-For"); xf != "" {
        parts := strings.Split(xf, ",")
        return strings.TrimSpace(parts[0])
    }
    host, _, _ := net.SplitHostPort(r.RemoteAddr)
    if host == "" {
        return "unknown"
    }
    return host
}
