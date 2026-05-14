package handlers

import (
	"encoding/json"
	"io"
	"net/http"
	"strings"

	"log"

	"github.com/OkaSher/Micro/protos/services/order-service/internal/usecase"
	"github.com/google/uuid"
)

type Handler struct{ uc *usecase.OrderUsecase }

func New(uc *usecase.OrderUsecase) *Handler { return &Handler{uc: uc} }

// OrderHandler handles GET /orders/{id}, POST /orders, and PUT /orders/{id}/status
func (h *Handler) OrderHandler(w http.ResponseWriter, r *http.Request) {
	// логировать входящий запрос
	log.Printf("%s %s from %s", r.Method, r.URL.Path, r.RemoteAddr)

	parts := strings.Split(strings.Trim(r.URL.Path, "/"), "/")
	if len(parts) < 1 || parts[0] != "orders" {
		http.NotFound(w, r)
		return
	}

	ctx := r.Context()

	switch r.Method {
	case http.MethodPost:
		// POST /orders - create new order
		if len(parts) != 1 {
			http.Error(w, "invalid path for POST", http.StatusBadRequest)
			return
		}
		var payload struct {
			Status string `json:"status"`
		}
		body, _ := io.ReadAll(r.Body)
		if err := json.Unmarshal(body, &payload); err != nil {
			http.Error(w, "invalid JSON", http.StatusBadRequest)
			return
		}
		if payload.Status == "" {
			payload.Status = "pending"
		}

		orderID := uuid.NewString()
		if err := h.uc.CreateOrder(ctx, orderID, payload.Status); err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}

		// после создания заказа
		log.Printf("created order id=%s status=%s", orderID, payload.Status)

		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusCreated)
		json.NewEncoder(w).Encode(map[string]string{"id": orderID, "status": payload.Status})
		return

	case http.MethodGet:
		// GET /orders/{id}
		if len(parts) < 2 {
			http.Error(w, "order id required", http.StatusBadRequest)
			return
		}
		id := parts[1]
		o, err := h.uc.GetOrder(ctx, id)
		if err != nil {
			http.Error(w, err.Error(), http.StatusNotFound)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(o)
		return

	case http.MethodPut:
		// PUT /orders/{id}/status
		if len(parts) < 2 {
			http.Error(w, "order id required", http.StatusBadRequest)
			return
		}
		id := parts[1]
		if len(parts) >= 3 && parts[2] == "status" {
			body, _ := io.ReadAll(r.Body)
			var payload struct {
				Status string `json:"status"`
			}
			_ = json.Unmarshal(body, &payload)
			if payload.Status == "" {
				http.Error(w, "missing status", http.StatusBadRequest)
				return
			}
			if err := h.uc.UpdateStatus(ctx, id, payload.Status); err != nil {
				http.Error(w, err.Error(), http.StatusInternalServerError)
				return
			}
			w.WriteHeader(http.StatusNoContent)
			return
		}
		fallthrough
	default:
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
}
