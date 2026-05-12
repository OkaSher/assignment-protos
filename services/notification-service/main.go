package main

import (
	"context"
	"log"
	"os"
	"os/signal"
	"strconv"
	"syscall"
	"time"

	"github.com/joho/godotenv"
	"github.com/redis/go-redis/v9"

	"github.com/OkaSher/Micro/protos/services/notification-service/internal/email"
	"github.com/OkaSher/Micro/protos/services/notification-service/internal/worker"
)

func main() {
	_ = godotenv.Load()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	redisDSN := getenv("REDIS_DSN", "redis:6379")
	rdb := redis.NewClient(&redis.Options{Addr: redisDSN})
	defer rdb.Close()

	provider := getenv("PROVIDER_MODE", "SIMULATED")
	var sender email.EmailSender
	if provider == "REAL" {
		sender = email.NewSMTPAdapter(getenv("SMTP_ADDR", "smtp:25"), getenv("SMTP_USER", ""), getenv("SMTP_PASS", ""))
	} else {
		sender = email.NewSimulatedAdapter()
	}

	retryCount := getenvInt("EMAIL_RETRY_COUNT", 3)
	baseMS := getenvInt("EMAIL_RETRY_BASE_MS", 2000)

	w := worker.New(rdb, sender, retryCount, baseMS)
	go w.Run(ctx)

	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)
	<-sigCh
	log.Println("notification-service shutting down")
	cancel()
	// allow worker to exit
	time.Sleep(500 * time.Millisecond)
}

func getenv(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}

func getenvInt(key string, fallback int) int {
	if v := os.Getenv(key); v != "" {
		if i, err := strconv.Atoi(v); err == nil {
			return i
		}
	}
	return fallback
}
