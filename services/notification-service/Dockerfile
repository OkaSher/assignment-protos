FROM golang:1.25-alpine AS build
WORKDIR /src

COPY go.mod go.sum ./
RUN go mod download

COPY . .
RUN CGO_ENABLED=0 GOOS=linux go build -o /out/notification-service ./services/notification-service

FROM alpine:3.21
WORKDIR /app
RUN adduser -D appuser
COPY --from=build /out/notification-service /usr/local/bin/notification-service
USER appuser
ENTRYPOINT ["/usr/local/bin/notification-service"]
