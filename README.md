# Microservices Production Hardening

This repository now includes Redis-backed caching, rate limiting, and notification retry logic.

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
