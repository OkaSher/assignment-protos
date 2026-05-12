package handlers

import (
    "encoding/json"
    "io"
    "net/http"
    "strings"

    "github.com/OkaSher/Micro/protos/services/order-service/internal/usecase"
)

type Handler struct{ uc *usecase.OrderUsecase }

func New(uc *usecase.OrderUsecase) *Handler { return &Handler{uc: uc} }

// OrderHandler handles GET /orders/{id} and PUT /orders/{id}/status
func (h *Handler) OrderHandler(w http.ResponseWriter, r *http.Request) {
    // path expected /orders/{id} or /orders/{id}/status
    parts := strings.Split(strings.Trim(r.URL.Path, "/"), "/")
    if len(parts) < 2 || parts[0] != "orders" {
        http.NotFound(w, r)
        return
    }
    id := parts[1]
    ctx := r.Context()

    switch r.Method {
    case http.MethodGet:
        o, err := h.uc.GetOrder(ctx, id)
        if err != nil {
            http.Error(w, err.Error(), http.StatusNotFound)
            return
        }
        w.Header().Set("Content-Type", "application/json")
        _ = json.NewEncoder(w).Encode(o)
        return
    case http.MethodPut:
        // expect path /orders/{id}/status
        if len(parts) >= 3 && parts[2] == "status" {
            body, _ := io.ReadAll(r.Body)
            var payload struct{ Status string `json:"status"` }
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
