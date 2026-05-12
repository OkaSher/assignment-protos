package worker

import (
    "context"
    "encoding/json"
    "log"
    "math"
    "time"

    "github.com/redis/go-redis/v9"

    "github.com/OkaSher/Micro/protos/services/notification-service/internal/email"
)

type paymentEvent struct {
    PaymentID     string `json:"payment_id"`
    TransactionID string `json:"transaction_id"`
    OrderID       string `json:"order_id"`
    Amount        int64  `json:"amount"`
    CustomerEmail string `json:"customer_email"`
}

type Worker struct{
    rdb *redis.Client
    sender email.EmailSender
    retries int
    baseMS int
}

func New(rdb *redis.Client, sender email.EmailSender, retries, baseMS int) *Worker {
    return &Worker{rdb: rdb, sender: sender, retries: retries, baseMS: baseMS}
}

func (w *Worker) Run(ctx context.Context) {
    sub := w.rdb.Subscribe(ctx, "payments:completed")
    ch := sub.Channel()
    for {
        select {
        case <-ctx.Done():
            return
        case msg, ok := <-ch:
            if !ok { return }
            var ev paymentEvent
            if err := json.Unmarshal([]byte(msg.Payload), &ev); err != nil {
                log.Printf("invalid event: %v", err)
                continue
            }
            go w.processEvent(ctx, ev)
        }
    }
}

func (w *Worker) processEvent(ctx context.Context, ev paymentEvent) {
    key := "notified:payment:" + ev.PaymentID
    ok, err := w.rdb.SetNX(ctx, key, "1", 24*time.Hour).Result()
    if err != nil {
        log.Printf("redis error: %v", err)
        return
    }
    if !ok {
        log.Printf("event already processed: %s", ev.PaymentID)
        return
    }

    attempt := 0
    for {
        attempt++
        err := w.sender.Send(ev.CustomerEmail, "Payment Received", "Your payment processed", ev.PaymentID)
        if err == nil {
            log.Printf("email sent for payment %s", ev.PaymentID)
            return
        }
        if attempt >= w.retries {
            log.Printf("giving up after %d attempts for %s: %v", attempt, ev.PaymentID, err)
            return
        }
        backoff := time.Duration(float64(w.baseMS) * math.Pow(2, float64(attempt-1)))
        time.Sleep(backoff * time.Millisecond)
    }
}
