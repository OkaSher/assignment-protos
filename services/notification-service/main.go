package main

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"os"
	"os/signal"
	"sync"
	"syscall"
	"time"

	amqp "github.com/rabbitmq/amqp091-go"
)

type paymentEvent struct {
	EventID       string `json:"event_id"`
	TransactionID string `json:"transaction_id"`
	OrderID       string `json:"order_id"`
	Amount        int64  `json:"amount"`
	CustomerEmail string `json:"customer_email"`
	Status        string `json:"status"`
	ProcessedAt   string `json:"processed_at"`
}

type idempotencyStore struct {
	mu   sync.Mutex
	seen map[string]struct{}
}

func (s *idempotencyStore) MarkIfNew(id string) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, ok := s.seen[id]; ok {
		return false
	}
	s.seen[id] = struct{}{}
	return true
}

func main() {
	rabbitURL := getenv("RABBITMQ_URL", "amqp://guest:guest@rabbitmq:5672/")
	queue := getenv("PAYMENT_QUEUE", "payment.completed")
	consumerTag := getenv("CONSUMER_TAG", "notification-service")

	conn, ch := mustOpenAMQP(rabbitURL)
	defer conn.Close()
	defer ch.Close()

	if _, err := ch.QueueDeclare(queue, true, false, false, false, nil); err != nil {
		log.Fatalf("queue declare failed: %v", err)
	}

	messages, err := ch.Consume(queue, consumerTag, false, false, false, false, nil)
	if err != nil {
		log.Fatalf("consume failed: %v", err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	store := &idempotencyStore{seen: make(map[string]struct{})}
	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		consumeLoop(ctx, messages, store)
	}()

	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)
	<-sigCh
	log.Println("notification-service shutting down")
	cancel()
	_ = ch.Cancel(consumerTag, false)
	wg.Wait()
}

func consumeLoop(ctx context.Context, messages <-chan amqp.Delivery, store *idempotencyStore) {
	for {
		select {
		case <-ctx.Done():
			return
		case d, ok := <-messages:
			if !ok {
				return
			}
			if err := handleMessage(d, store); err != nil {
				log.Printf("message failed: %v", err)
				_ = d.Nack(false, true)
				continue
			}
			if err := d.Ack(false); err != nil {
				log.Printf("ack failed: %v", err)
			}
		}
	}
}

func handleMessage(d amqp.Delivery, store *idempotencyStore) error {
	var event paymentEvent
	if err := json.Unmarshal(d.Body, &event); err != nil {
		return fmt.Errorf("invalid payload: %w", err)
	}

	id := d.MessageId
	if id == "" {
		id = event.EventID
	}
	if id == "" {
		id = event.TransactionID
	}
	if id == "" {
		return fmt.Errorf("missing idempotency key")
	}

	if !store.MarkIfNew(id) {
		log.Printf("[Notification] Duplicate event skipped: %s", id)
		return nil
	}

	log.Printf("[Notification] Sent email to %s for Order #%s. Amount: $%.2f", event.CustomerEmail, event.OrderID, float64(event.Amount)/100)
	return nil
}

func mustOpenAMQP(url string) (*amqp.Connection, *amqp.Channel) {
	var conn *amqp.Connection
	var ch *amqp.Channel
	var err error
	for i := 1; i <= 20; i++ {
		conn, err = amqp.Dial(url)
		if err == nil {
			ch, err = conn.Channel()
		}
		if err == nil {
			return conn, ch
		}
		if conn != nil {
			_ = conn.Close()
		}
		log.Printf("waiting for rabbitmq (%d/20): %v", i, err)
		time.Sleep(2 * time.Second)
	}
	log.Fatalf("rabbitmq unavailable: %v", err)
	return nil, nil
}

func getenv(key, fallback string) string {
	if value := os.Getenv(key); value != "" {
		return value
	}
	return fallback
}
