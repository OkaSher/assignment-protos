package main

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"log"
	"net"
	"os"
	"os/signal"
	"syscall"
	"time"

	pb "github.com/OkaSher/Micro/protos/generated-repo/payment"
	"github.com/google/uuid"
	amqp "github.com/rabbitmq/amqp091-go"
	"google.golang.org/grpc"
	"google.golang.org/grpc/metadata"
	"google.golang.org/protobuf/types/known/timestamppb"

	_ "github.com/lib/pq"
)

type server struct {
	pb.UnimplementedPaymentServiceServer
	db    *sql.DB
	amqp  *amqp.Channel
	queue string
}

type paymentEvent struct {
	EventID       string `json:"event_id"`
	TransactionID string `json:"transaction_id"`
	OrderID       string `json:"order_id"`
	Amount        int64  `json:"amount"`
	CustomerEmail string `json:"customer_email"`
	Status        string `json:"status"`
	ProcessedAt   string `json:"processed_at"`
}

func main() {
	grpcPort := getenv("GRPC_PORT", "50051")
	dsn := getenv("POSTGRES_DSN", "host=db port=5432 user=postgres password=postgres dbname=payments sslmode=disable")
	rabbitURL := getenv("RABBITMQ_URL", "amqp://guest:guest@rabbitmq:5672/")
	queue := getenv("PAYMENT_QUEUE", "payment.completed")

	db := mustOpenDB(dsn)
	defer db.Close()

	mustRunMigrations(db)

	conn, ch := mustOpenAMQP(rabbitURL)
	defer conn.Close()
	defer ch.Close()

	if _, err := ch.QueueDeclare(queue, true, false, false, false, nil); err != nil {
		log.Fatalf("queue declare failed: %v", err)
	}

	lis, err := net.Listen("tcp", ":"+grpcPort)
	if err != nil {
		log.Fatalf("listen failed: %v", err)
	}

	grpcServer := grpc.NewServer()
	pb.RegisterPaymentServiceServer(grpcServer, &server{db: db, amqp: ch, queue: queue})

	go func() {
		log.Printf("payment-service gRPC listening on :%s", grpcPort)
		if err := grpcServer.Serve(lis); err != nil {
			log.Fatalf("gRPC serve failed: %v", err)
		}
	}()

	waitForShutdown(func() {
		log.Println("payment-service shutting down")
		grpcServer.GracefulStop()
	})
}

func (s *server) ProcessPayment(ctx context.Context, req *pb.PaymentRequest) (*pb.PaymentResponse, error) {
	if req.GetOrderId() == "" {
		return nil, fmt.Errorf("order_id is required")
	}
	if req.GetAmount() <= 0 {
		return nil, fmt.Errorf("amount must be > 0")
	}

	customerEmail := "user@example.com"
	if md, ok := metadata.FromIncomingContext(ctx); ok {
		if values := md.Get("customer-email"); len(values) > 0 && values[0] != "" {
			customerEmail = values[0]
		}
	}

	transactionID := uuid.NewString()
	status := "completed"
	processedAt := time.Now().UTC()

	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, fmt.Errorf("begin tx: %w", err)
	}

	_, err = tx.ExecContext(ctx, `
		INSERT INTO payments (transaction_id, order_id, amount, customer_email, status, processed_at)
		VALUES ($1, $2, $3, $4, $5, $6)
	`, transactionID, req.GetOrderId(), req.GetAmount(), customerEmail, status, processedAt)
	if err != nil {
		_ = tx.Rollback()
		return nil, fmt.Errorf("insert payment: %w", err)
	}

	if err := tx.Commit(); err != nil {
		return nil, fmt.Errorf("commit tx: %w", err)
	}

	event := paymentEvent{
		EventID:       uuid.NewString(),
		TransactionID: transactionID,
		OrderID:       req.GetOrderId(),
		Amount:        req.GetAmount(),
		CustomerEmail: customerEmail,
		Status:        status,
		ProcessedAt:   processedAt.Format(time.RFC3339),
	}
	body, err := json.Marshal(event)
	if err != nil {
		return nil, fmt.Errorf("marshal event: %w", err)
	}

	if err := s.amqp.PublishWithContext(ctx, "", s.queue, false, false, amqp.Publishing{
		ContentType:  "application/json",
		DeliveryMode: amqp.Persistent,
		MessageId:    event.EventID,
		Timestamp:    processedAt,
		Body:         body,
	}); err != nil {
		return nil, fmt.Errorf("publish event: %w", err)
	}

	return &pb.PaymentResponse{
		TransactionId: transactionID,
		Status:        status,
		OrderId:       req.GetOrderId(),
		ProcessedAt:   timestamppb.New(processedAt),
	}, nil
}

func (s *server) ListPayments(ctx context.Context, req *pb.ListPaymentsRequest) (*pb.ListPaymentsResponse, error) {
	query := `SELECT transaction_id, status, order_id, processed_at FROM payments`
	args := []any{}
	if req.GetStatus() != "" {
		query += ` WHERE status = $1`
		args = append(args, req.GetStatus())
	}
	query += ` ORDER BY processed_at DESC`

	rows, err := s.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	resp := &pb.ListPaymentsResponse{}
	for rows.Next() {
		var txnID, status, orderID string
		var processedAt time.Time
		if err := rows.Scan(&txnID, &status, &orderID, &processedAt); err != nil {
			return nil, err
		}
		resp.Payments = append(resp.Payments, &pb.PaymentResponse{
			TransactionId: txnID,
			Status:        status,
			OrderId:       orderID,
			ProcessedAt:   timestamppb.New(processedAt),
		})
	}
	return resp, rows.Err()
}

func mustOpenDB(dsn string) *sql.DB {
	var db *sql.DB
	var err error
	for i := 1; i <= 20; i++ {
		db, err = sql.Open("postgres", dsn)
		if err == nil {
			err = db.Ping()
		}
		if err == nil {
			return db
		}
		log.Printf("waiting for postgres (%d/20): %v", i, err)
		time.Sleep(2 * time.Second)
	}
	log.Fatalf("postgres unavailable: %v", err)
	return nil
}

func mustRunMigrations(db *sql.DB) {
	_, err := db.Exec(`
		CREATE TABLE IF NOT EXISTS payments (
			transaction_id TEXT PRIMARY KEY,
			order_id TEXT NOT NULL,
			amount BIGINT NOT NULL,
			customer_email TEXT NOT NULL,
			status TEXT NOT NULL,
			processed_at TIMESTAMPTZ NOT NULL
		)
	`)
	if err != nil {
		log.Fatalf("migration failed: %v", err)
	}
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

func waitForShutdown(shutdown func()) {
	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)
	<-sigCh
	shutdown()
}

func getenv(key, fallback string) string {
	if value := os.Getenv(key); value != "" {
		return value
	}
	return fallback
}
