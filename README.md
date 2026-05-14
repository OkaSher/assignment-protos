# Microservices Production Hardening

This repository now includes Redis-backed caching, rate limiting, and notification retry logic.

## Architecture Diagram

```
┌─────────────────────────────────────────────────────────────────────────────────────┐
│                                   CLIENT REQUESTS                                   │
└────────────────────────────┬────────────────────────────────────────────────────────┘
                             │
                             ▼
┌──────────────────────────────────────────────────────────────────────────────────────┐
│                         RATE LIMITER MIDDLEWARE (Redis)                              │
│                  (10 req/min per IP, returns 429 if exceeded)                        │
└────────────────────────────┬────────────────────────────────────────────────────────┘
                             │
        ┌────────────────────┼────────────────────┐
        │                    │                    │
        ▼                    ▼                    ▼
   [GET /orders/]    [PUT /orders/]      [GET /healthz]
        │                    │
        └────────┬───────────┴─────────┐
                 │                     │
                 ▼                     ▼
        ┌─────────────────────────────────────────┐
        │     ORDER SERVICE (HTTP Port 8080)      │
        │                                         │
        │  ┌────────────────────────────────────┐ │
        │  │   CACHE-ASIDE PATTERN              │ │
        │  │                                    │ │
        │  │  GET /orders/:id                   │ │
        │  │  1. Check Redis (key: order:id)    │ │
        │  │  2. On miss → Query PostgreSQL     │ │
        │  │  3. Store in Redis (TTL: 5 min)    │ │
        │  │  4. Return to client               │ │
        │  │                                    │ │
        │  │  PUT /orders/:id/status            │ │
        │  │  1. Update PostgreSQL              │ │
        │  │  2. Delete Redis key (invalidate)  │ │
        │  │  3. Return 204 No Content          │ │
        │  └────────────────────────────────────┘ │
        │                                         │
        │  ┌─────────────┐      ┌──────────────┐  │
        │  │    Redis    │◄────►│ PostgreSQL   │  │
        │  │   (Cache)   │      │   (Orders)   │  │
        │  └─────────────┘      └──────────────┘  │
        └─────────────────────────────────────────┘
                     │                │
                     └─────────┬──────┘
                               │
        ┌──────────────────────┴──────────────────────┐
        │                                             │
        ▼                                             ▼
┌──────────────────────┐                    ┌──────────────────────┐
│   REDIS (Cache)      │                    │  PostgreSQL (Orders) │
│                      │                    │                      │
│ • order:id (5 min)   │                    │ • orders table       │
│ • notified:payment:id│                    │ • status tracking    │
│ • rl:ip:timestamp    │                    │                      │
└──────────────────────┘                    └──────────────────────┘
        ▲                                              
        │                                              
        │                          ┌──────────────────────────────────────┐
        │                          │  PAYMENT SERVICE (gRPC Port 50051)   │
        │                          │                                      │
        │                          │ Processes payment, publishes event   │
        │                          │ to RabbitMQ payments:completed       │
        │                          └──────────────┬───────────────────────┘
        │                                         │
        │                          ┌──────────────▼───────────────────────┐
        │                          │     RABBITMQ (Event Broker)          │
        │                          │                                      │
        │                          │ Topic: payments:completed            │
        │                          │ (or Redis Stream alternative)        │
        │                          └──────────────┬───────────────────────┘
        │                                         │
        │                          ┌──────────────▼───────────────────────┐
        │                          │  NOTIFICATION SERVICE WORKER         │
        │                          │                                      │
        │  ┌────────────────────────────────────────────────────────────┐ │
        │  │  BACKGROUND JOB PIPELINE (Redis Subscription)             │ │
        │  │                                                            │ │
        │  │  1. Subscribe to payments:completed channel               │ │
        │  │  2. On event received:                                    │ │
        │  │     a. Extract payment_id from event                      │ │
        │  │     b. Check Redis SETNX(notified:payment:id, "1")        │ │
        │  │        - If already set → Skip (idempotency)              │ │
        │  │        - If new → Proceed                                 │ │
        │  │     c. Attempt to send email (adapter pattern):           │ │
        │  │        - REAL: SMTP adapter                               │ │
        │  │        - SIMULATED: Mock with latency + random errors     │ │
        │  │                                                            │ │
        │  │  3. On error → Exponential Backoff Retry:                 │ │
        │  │     Attempt 1: Wait 2s  → Retry                          │ │
        │  │     Attempt 2: Wait 4s  → Retry                          │ │
        │  │     Attempt 3: Wait 8s  → Retry                          │ │
        │  │     Attempt N: Give up after EMAIL_RETRY_COUNT            │ │
        │  │                                                            │ │
        │  │  4. On success → Log and proceed to next event            │ │
        │  └────────────────────────────────────────────────────────────┘ │
        │                                                                  │
        │  ┌─────────────────────────────────────────────────────────────┐│
        │  │           EMAIL ADAPTER INTERFACE                          ││
        │  │                                                             ││
        │  │  • EmailSender (interface)                                 ││
        │  │    ├─ Send(to, subject, body, id) error                    ││
        │  │    │                                                       ││
        │  │    ├─ SMTPAdapter (REAL mode)                              ││
        │  │    │  └─ Integrates with SMTP server                      ││
        │  │    │                                                       ││
        │  │    └─ SimulatedAdapter (SIMULATED mode)                    ││
        │  │       ├─ Adds 200–1000ms latency                           ││
        │  │       ├─ Returns random errors (20% failure)               ││
        │  │       └─ Logs mock delivery                                ││
        │  └─────────────────────────────────────────────────────────────┘│
        │                                                                  │
        │  Redis:                                                         │
        │  • notified:payment:id (24h TTL) → Idempotency store          │
        └──────────────────────────────────────────────────────────────────┘


FLOW SUMMARY:

  Order Service Read Flow:
    Client → Rate Limiter → GET /orders/:id → Redis (Hit/Miss) → [PostgreSQL] → Client

  Order Service Write Flow:
    Client → Rate Limiter → PUT /orders/:id/status → PostgreSQL → Delete Redis Key → Client

  Notification Background Job Flow:
    Payment Event → RabbitMQ/Redis → Worker → Idempotency Check → Email Adapter → [SMTP/Mock]
                                                       ↓ (retry on error)
                                            Exponential Backoff (2s, 4s, 8s...)
```

## Order Service

- Uses a cache-aside pattern for `GET /orders/:id`.
- The service checks Redis first and falls back to PostgreSQL on a miss.
- Cached order entries expire after 5 minutes (`CACHE_TTL_SEC=300`).
- When an order status changes through `PUT /orders/:id/status`, the service updates PostgreSQL and deletes the Redis key for that order.
- A Redis-backed rate limiter blocks clients after 10 requests per minute by IP and returns `429 Too Many Requests`.

## Notification Service

- The `EmailSender` interface hides the delivery mechanism behind adapters.
- `PROVIDER_MODE=REAL` selects SMTP.
- `PROVIDER_MODE=SIMULATED` uses a mock adapter that adds latency and may return random failures.
- Before sending, the worker writes an idempotency key in Redis using `SETNX` semantics so the same `payment_id` is not processed twice.
- If the provider fails, the worker retries with exponential backoff: `2s`, `4s`, `8s`, and so on up to the configured retry count.

## Configuration

All runtime settings are loaded from `.env` when available and can also be provided through the environment in Docker:

- `POSTGRES_DSN`
- `REDIS_DSN`
- `CACHE_TTL_SEC`
- `RATE_LIMIT_PER_MIN`
- `PROVIDER_MODE`
- `EMAIL_RETRY_COUNT`
- `EMAIL_RETRY_BASE_MS`

---

## Testing & Verification Checklist

Use these commands to verify all requirements are implemented:

### 1. **Build the Project**

```bash
cd c:\Users\Admin\assignment-protos
go mod tidy
go build ./...
```

If both commands succeed with no errors, all Go dependencies and imports are correct.

### 2. **Verify Source Code Structure**

```bash
# Check order-service Redis caching implementation
dir services\order-service\internal\cache\
dir services\order-service\internal\repo\
dir services\order-service\internal\usecase\
dir services\order-service\middleware\
dir services\order-service\transport\http\

# Check notification-service adapters and worker
dir services\notification-service\internal\email\
dir services\notification-service\internal\worker\
```

**Expected files:**
- `order-service/internal/cache/redis_cache.go` → Cache-aside pattern with TTL
- `order-service/internal/usecase/order.go` → Cache-aside logic + invalidation
- `order-service/middleware/rate_limit.go` → Redis rate limiter (10 req/min)
- `notification-service/internal/email/email.go` → EmailSender interface + adapters (REAL/SIMULATED)
- `notification-service/internal/worker/worker.go` → Background jobs + idempotency + exponential backoff

### 3. **Verify Docker Compose Configuration**

```bash
grep -A 5 -e "redis:" docker-compose.yml
```

Should show:
```yaml
redis:
  image: redis:7-alpine
  healthcheck:
    test: ["CMD", "redis-cli", "ping"]
```

### 4. **Verify Environment Variables**

Check `.env` file:
```bash
cat .env
```

Should contain:
```
POSTGRES_DSN=host=db port=5432 user=postgres password=postgres dbname=payments sslmode=disable
REDIS_DSN=redis:6379
CACHE_TTL_SEC=300
RATE_LIMIT_PER_MIN=10
PROVIDER_MODE=SIMULATED
EMAIL_RETRY_COUNT=3
EMAIL_RETRY_BASE_MS=2000
```

### 5. **Start Infrastructure (Docker Compose)**

```bash
docker-compose up -d
```

Wait for all services to be healthy:
```bash
docker-compose ps
```

All containers should show `healthy` or `running`.

### 6. **Test Order Service - Cache-Aside Pattern**

```powershell
# Test 1: GET order (first time - cache miss, queries PostgreSQL)
Invoke-WebRequest -Uri "http://localhost:8080/orders/1" -Method GET

# Test 2: GET same order (cache hit - Redis)
Invoke-WebRequest -Uri "http://localhost:8080/orders/1" -Method GET
# Should be faster (cached)

# Check Redis cache key directly
docker-compose exec redis redis-cli
> KEYS order:*
> GET order:1
> EXIT
```

### 7. **Test Order Service - Cache Invalidation**

```powershell
# Update order status (invalidates cache)
Invoke-WebRequest -Uri "http://localhost:8080/orders/1/status" `
  -Method PUT `
  -ContentType "application/json" `
  -Body '{"status":"shipped"}'

# Verify cache was deleted
docker-compose exec redis redis-cli KEYS "order:1"
# Should return nothing (empty)
```

### 8. **Test Rate Limiter (429 Too Many Requests)**

```powershell
# Send 11 requests quickly (limit is 10 per minute per IP)
for ($i=1; $i -le 11; $i++) {
  try {
    $r = Invoke-WebRequest -Uri "http://localhost:8080/healthz" -Method GET
    Write-Host ("Request {0}: HTTP {1}" -f $i, $r.StatusCode)
  } catch {
    $code = $_.Exception.Response.StatusCode.value__
    Write-Host ("Request {0}: HTTP {1}" -f $i, $code)
  }
  Start-Sleep -Milliseconds 100
}

# You should see the last request return 429
```

### 9. **Test Health Endpoints**

```powershell
# Order Service health
Invoke-WebRequest -Uri "http://localhost:8080/healthz" -Method GET

# Check service status
docker-compose ps
```

### 10. **Test Exponential Backoff Retry**

```powershell
# Enable SIMULATED mode with frequent failures
$env:PROVIDER_MODE="SIMULATED"
$env:EMAIL_RETRY_COUNT="3"
$env:EMAIL_RETRY_BASE_MS="2000"

docker-compose up -d notification-service

# Logs should show retries with increasing delays:
# Attempt 1: Send
# (wait 2s)
# Attempt 2: Send
# (wait 4s)
# Attempt 3: Send
# (give up or succeed)
docker-compose logs notification-service
```

### 11. **Full Integration Test (Docker Compose)**

```powershell
# Terminal 1: Start all services
docker-compose up

# Terminal 2: Run test sequence
# 1. Get health
Invoke-WebRequest -Uri "http://localhost:8080/healthz" -Method GET

# 2. Get order (cache miss)
Invoke-WebRequest -Uri "http://localhost:8080/orders/test-order-1" -Method GET

# 3. Get order again (cache hit)
Invoke-WebRequest -Uri "http://localhost:8080/orders/test-order-1" -Method GET

# 4. Update status (invalidates cache)
Invoke-WebRequest -Uri "http://localhost:8080/orders/test-order-1/status" `
  -Method PUT `
  -ContentType "application/json" `
  -Body '{"status":"completed"}'

# 5. Verify logs
docker-compose logs order-service
docker-compose logs notification-service
docker-compose logs redis
```

### 12. **Create New Order (POST)**

```powershell
# Create a new order
$response = Invoke-WebRequest -Uri "http://localhost:8080/orders" `
  -Method POST `
  -ContentType "application/json" `
  -Body '{"status":"pending"}'

$orderId = ($response.Content | ConvertFrom-Json).id
Write-Host "Created order: $orderId"

# Verify it was created
Invoke-WebRequest -Uri "http://localhost:8080/orders/$orderId" -Method GET
```

### 13. **Verify All Services Healthy**

```powershell
# Check status of all containers
docker-compose ps

# All should show (healthy) status
```

### 14. **Cleanup**

```bash
docker-compose down -v
```

---

## Summary of Implementation

✅ **Order Service:**
- Redis cache-aside pattern with 5-minute TTL
- Cache invalidation on status update
- Rate limiter middleware (10 req/min per IP, returns 429)

✅ **Notification Service:**
- EmailSender interface with REAL (SMTP) and SIMULATED adapters
- PROVIDER_MODE environment variable to select adapter
- Redis-backed idempotency (SETNX prevents duplicates)
- Exponential backoff retry policy (2s, 4s, 8s...)

✅ **Infrastructure:**
- Redis container in docker-compose.yml
- PostgreSQL for persistent storage
- RabbitMQ for event streaming
- All configurations loaded from .env

✅ **Documentation:**
- Architecture diagrams (ASCII + Mermaid)
- Cache invalidation strategy explained
- Retry and idempotency logic documented
