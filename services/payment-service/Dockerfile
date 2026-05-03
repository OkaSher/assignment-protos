FROM golang:1.25-alpine AS build
WORKDIR /src

COPY go.mod go.sum ./
RUN go mod download

COPY . .
RUN CGO_ENABLED=0 GOOS=linux go build -o /out/payment-service ./services/payment-service

FROM alpine:3.21
WORKDIR /app
RUN adduser -D appuser
COPY --from=build /out/payment-service /usr/local/bin/payment-service
USER appuser
EXPOSE 50051
ENTRYPOINT ["/usr/local/bin/payment-service"]
