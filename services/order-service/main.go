package main

import (
	"database/sql"
	"fmt"
	"log"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/joho/godotenv"
	_ "github.com/lib/pq"
	"github.com/redis/go-redis/v9"

	cachepkg "github.com/OkaSher/Micro/protos/services/order-service/internal/cache"
	"github.com/OkaSher/Micro/protos/services/order-service/internal/repo"
	"github.com/OkaSher/Micro/protos/services/order-service/internal/usecase"
	"github.com/OkaSher/Micro/protos/services/order-service/middleware"
	handlers "github.com/OkaSher/Micro/protos/services/order-service/transport/http/handlers"
)

func main() {
	_ = godotenv.Load()

	dbDSN := getenv("POSTGRES_DSN", "host=db port=5432 user=postgres password=postgres dbname=payments sslmode=disable")
	cacheDSN := getenv("REDIS_DSN", "redis:6379")

	db, err := sql.Open("postgres", dbDSN)
	if err != nil {
		log.Fatalf("opening db: %v", err)
	}
	defer db.Close()

	rdb := redis.NewClient(&redis.Options{Addr: cacheDSN})
	defer rdb.Close()

	repoSvc := repo.NewPostgresRepo(db)
	ttlSec := getenvInt("CACHE_TTL_SEC", 300)
	cacheSvc := cachepkg.NewRedisCache(rdb, time.Duration(ttlSec)*time.Second)
	uc := usecase.NewOrderUsecase(repoSvc, cacheSvc)

	h := handlers.New(uc)

	mux := http.NewServeMux()
	mux.HandleFunc("/healthz", func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusOK) })
	mux.HandleFunc("/orders/", h.OrderHandler)

	rlPerMin := getenvInt("RATE_LIMIT_PER_MIN", 10)
	handler := middleware.RateLimitMiddleware(rdb, rlPerMin)(mux)

	srv := &http.Server{Addr: ":" + getenv("HTTP_PORT", "8080"), Handler: handler}

	go func() {
		log.Printf("order-service HTTP listening on %s", srv.Addr)
		if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			log.Fatalf("http serve failed: %v", err)
		}
	}()

	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)
	<-sigCh
	log.Println("order-service shutting down")
	_ = srv.Close()
}

func getenv(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}

func getenvInt(key string, fallback int) int {
	if v := os.Getenv(key); v != "" {
		var i int
		_, err := fmt.Sscanf(v, "%d", &i)
		if err == nil {
			return i
		}
	}
	return fallback
}
