package main

import (
	"context"
	"encoding/json"
	"log"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	pb "github.com/OkaSher/Micro/protos/generated-repo/payment"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/metadata"
)

type payRequest struct {
	OrderID       string `json:"order_id"`
	Amount        int64  `json:"amount"`
	CustomerEmail string `json:"customer_email"`
}

type payResponse struct {
	TransactionID string `json:"transaction_id"`
	Status        string `json:"status"`
	OrderID       string `json:"order_id"`
	ProcessedAt   string `json:"processed_at"`
}

func main() {
	httpAddr := ":" + getenv("HTTP_PORT", "8080")
	paymentAddr := getenv("PAYMENT_GRPC_ADDR", "payment-service:50051")

	conn, err := grpc.NewClient(paymentAddr, grpc.WithTransportCredentials(insecure.NewCredentials()))
	if err != nil {
		log.Fatalf("payment gRPC dial failed: %v", err)
	}
	defer conn.Close()

	client := pb.NewPaymentServiceClient(conn)
	mux := http.NewServeMux()

	mux.HandleFunc("/healthz", func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("ok"))
	})

	mux.HandleFunc("/orders/pay", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}

		var req payRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			http.Error(w, "invalid json", http.StatusBadRequest)
			return
		}
		if req.OrderID == "" || req.Amount <= 0 {
			http.Error(w, "order_id and positive amount are required", http.StatusBadRequest)
			return
		}
		if req.CustomerEmail == "" {
			req.CustomerEmail = "user@example.com"
		}

		ctx, cancel := context.WithTimeout(r.Context(), 5*time.Second)
		defer cancel()
		ctx = metadata.NewOutgoingContext(ctx, metadata.Pairs("customer-email", req.CustomerEmail))

		grpcResp, err := client.ProcessPayment(ctx, &pb.PaymentRequest{
			OrderId: req.OrderID,
			Amount:  req.Amount,
		})
		if err != nil {
			http.Error(w, "payment failed: "+err.Error(), http.StatusBadGateway)
			return
		}

		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(payResponse{
			TransactionID: grpcResp.GetTransactionId(),
			Status:        grpcResp.GetStatus(),
			OrderID:       grpcResp.GetOrderId(),
			ProcessedAt:   grpcResp.GetProcessedAt().AsTime().Format(time.RFC3339),
		})
	})

	httpServer := &http.Server{
		Addr:              httpAddr,
		Handler:           mux,
		ReadHeaderTimeout: 5 * time.Second,
	}

	go func() {
		log.Printf("order-service HTTP listening on %s", httpAddr)
		if err := httpServer.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			log.Fatalf("http serve failed: %v", err)
		}
	}()

	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)
	<-sigCh
	log.Println("order-service shutting down")

	shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	_ = httpServer.Shutdown(shutdownCtx)
}

func getenv(key, fallback string) string {
	if value := os.Getenv(key); value != "" {
		return value
	}
	return fallback
}
