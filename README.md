# Assignment 3 - Asynchronous Notifications (RabbitMQ)

This repository now implements an event-driven flow:

Order Service -> Payment Service -> RabbitMQ -> Notification Service

## Services

- **Order Service** (`services/order-service`)
	- HTTP endpoint to trigger payment processing.
	- Calls Payment Service over gRPC.

- **Payment Service** (`services/payment-service`)
	- gRPC producer (`ProcessPayment`, `ListPayments`).
	- Stores committed payment data in PostgreSQL.
	- Publishes `payment.completed` events to RabbitMQ **after DB commit**.

- **Notification Service** (`services/notification-service`)
	- RabbitMQ consumer with **manual ACK** (`autoAck=false`).
	- Logs email simulation only after successful processing.
	- Uses idempotency tracking (in-memory event ID store) to skip duplicates.

Infrastructure (`docker-compose.yml`):

- RabbitMQ (broker)
- PostgreSQL (database)
- Order Service
- Payment Service
- Notification Service

Architecture diagram: [docs/architecture.md](docs/architecture.md)

## Event Payload (JSON)

`payment.completed` message payload:

```json
{
	"event_id": "uuid",
	"transaction_id": "uuid",
	"order_id": "123",
	"amount": 9999,
	"customer_email": "user@example.com",
	"status": "completed",
	"processed_at": "2026-05-02T12:00:00Z"
}
```

> `amount` is stored in cents. Example: `9999 = $99.99`.

## Reliability Notes

- **At-least-once delivery**: durable queue + persistent messages + manual ACK.
- **No auto-ack**: consumer acknowledges only after processing/logging succeeds.
- **Idempotent consumer**: repeated delivery with the same event ID is ignored.
- **Graceful shutdown**: each service handles `SIGINT`/`SIGTERM` and closes cleanly.

## Run

From the project root:

1. `docker compose up --build`
2. Trigger a payment via Order Service:

	 - `POST http://localhost:8080/orders/pay`
	 - JSON body:

	 ```json
	 {
		 "order_id": "123",
		 "amount": 9999,
		 "customer_email": "user@example.com"
	 }
	 ```

3. Check Notification Service logs for:

	 - `[Notification] Sent email to user@example.com for Order #123. Amount: $99.99`

## Design Decisions (Assignment Requirements Mapping)

- **Decoupling**: Notification Service does not call Order/Payment services.
- **Producer reliability**: Payment event is published only after DB transaction commit.
- **Manual ACKs**: Implemented in Notification consumer.
- **Idempotency**: Event IDs tracked in-memory to prevent duplicate side effects.
- **Docker orchestration**: all required components run via Compose.
