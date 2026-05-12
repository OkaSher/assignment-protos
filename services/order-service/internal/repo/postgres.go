package repo

import (
    "context"
    "database/sql"
    "encoding/json"

    "fmt"
)

type Order struct {
    ID     string `json:"id"`
    Status string `json:"status"`
    Data   string `json:"data"`
}

type PostgresRepo struct{ db *sql.DB }

func NewPostgresRepo(db *sql.DB) *PostgresRepo { return &PostgresRepo{db: db} }

func (p *PostgresRepo) GetOrder(ctx context.Context, id string) (*Order, error) {
    var o Order
    row := p.db.QueryRowContext(ctx, `SELECT id, status, data FROM orders WHERE id = $1 LIMIT 1`, id)
    if err := row.Scan(&o.ID, &o.Status, &o.Data); err != nil {
        if err == sql.ErrNoRows {
            return nil, fmt.Errorf("not found")
        }
        return nil, err
    }
    return &o, nil
}

func (p *PostgresRepo) UpdateStatus(ctx context.Context, id, status string) error {
    // simple update
    b, _ := json.Marshal(map[string]string{"status": status})
    _, err := p.db.ExecContext(ctx, `UPDATE orders SET status=$1, data=$2 WHERE id=$3`, status, string(b), id)
    return err
}
