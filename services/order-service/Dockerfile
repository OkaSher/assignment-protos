FROM golang:1.25-alpine AS build
WORKDIR /src

COPY go.mod go.sum ./
RUN go mod download

COPY . .
RUN CGO_ENABLED=0 GOOS=linux go build -o /out/order-service ./services/order-service

FROM alpine:3.21
WORKDIR /app
RUN adduser -D appuser
COPY --from=build /out/order-service /usr/local/bin/order-service
USER appuser
EXPOSE 8080
ENTRYPOINT ["/usr/local/bin/order-service"]
