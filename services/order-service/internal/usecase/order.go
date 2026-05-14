package usecase

import (
	"context"
	"encoding/json"

	"github.com/OkaSher/Micro/protos/services/order-service/internal/repo"
)

type OrderCache interface {
	Get(ctx context.Context, key string) (string, bool, error)
	Set(ctx context.Context, key, val string) error
	Delete(ctx context.Context, key string) error
}

type OrderRepo interface {
	GetOrder(ctx context.Context, id string) (*repo.Order, error)
	UpdateStatus(ctx context.Context, id, status string) error
	CreateOrder(ctx context.Context, id, status string) error
}

type OrderUsecase struct {
	repo  OrderRepo
	cache OrderCache
}

func NewOrderUsecase(r OrderRepo, c OrderCache) *OrderUsecase {
	return &OrderUsecase{repo: r, cache: c}
}

func (u *OrderUsecase) GetOrder(ctx context.Context, id string) (*repo.Order, error) {
	key := "order:" + id
	if v, ok, err := u.cache.Get(ctx, key); err == nil && ok {
		var o repo.Order
		if err := json.Unmarshal([]byte(v), &o); err == nil {
			return &o, nil
		}
	}
	// cache miss
	o, err := u.repo.GetOrder(ctx, id)
	if err != nil {
		return nil, err
	}
	b, _ := json.Marshal(o)
	_ = u.cache.Set(ctx, key, string(b))
	return o, nil
}

func (u *OrderUsecase) UpdateStatus(ctx context.Context, id, status string) error {
	if err := u.repo.UpdateStatus(ctx, id, status); err != nil {
		return err
	}
	// invalidate cache
	_ = u.cache.Delete(ctx, "order:"+id)
	return nil
}

func (u *OrderUsecase) CreateOrder(ctx context.Context, id, status string) error {
	// Insert to database
	if err := u.repo.CreateOrder(ctx, id, status); err != nil {
		return err
	}
	// Cache the new order
	o := &repo.Order{ID: id, Status: status, Data: "{}"}
	b, _ := json.Marshal(o)
	_ = u.cache.Set(ctx, "order:"+id, string(b))
	return nil
}
