package store

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"

	"catalogsync/internal/domain"
)

type ProductKey struct{ SupplierID, ProductID string }

const productColumns = `supplier_id,product_id,revision,status,name,price_cents,event_digest,updated_at`

func scanProduct(row scanner) (domain.Product, error) {
	var p domain.Product
	err := row.Scan(&p.SupplierID, &p.ProductID, &p.Revision, &p.Status, &p.Name, &p.PriceCents, &p.EventDigest, &p.UpdatedAt)
	return p, err
}
func ProductTx(ctx context.Context, tx *sql.Tx, key ProductKey) (*domain.Product, error) {
	p, err := scanProduct(tx.QueryRowContext(ctx, "SELECT "+productColumns+" FROM products WHERE supplier_id=? AND product_id=?", key.SupplierID, key.ProductID))
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("read product: %w", err)
	}
	return &p, nil
}
func (s *Store) Products(ctx context.Context, includeWithdrawn bool) (products []domain.Product, err error) {
	rows, err := s.db.QueryContext(ctx, "SELECT "+productColumns+" FROM products WHERE status='active' OR ? ORDER BY supplier_id,product_id", includeWithdrawn)
	if err != nil {
		return nil, fmt.Errorf("list products: %w", err)
	}
	defer func() { err = errors.Join(err, rows.Close()) }()
	products = []domain.Product{}
	for rows.Next() {
		p, e := scanProduct(rows)
		if e != nil {
			return nil, e
		}
		products = append(products, p)
	}
	return products, rows.Err()
}

type Effect struct {
	JobID      string
	BatchIndex int
	Kind       domain.Kind
	Old        *domain.Product
	New        domain.Product
}

func WriteEffect(ctx context.Context, tx *sql.Tx, e Effect) error {
	p := e.New
	p.UpdatedAt = timestamp()
	_, err := tx.ExecContext(ctx, `INSERT INTO products(`+productColumns+`) VALUES(?,?,?,?,?,?,?,?) ON CONFLICT(supplier_id,product_id) DO UPDATE SET revision=excluded.revision,status=excluded.status,name=excluded.name,price_cents=excluded.price_cents,event_digest=excluded.event_digest,updated_at=excluded.updated_at`, p.SupplierID, p.ProductID, p.Revision, p.Status, p.Name, p.PriceCents, p.EventDigest, p.UpdatedAt)
	if err != nil {
		return fmt.Errorf("write product: %w", err)
	}
	before, err := json.Marshal(e.Old)
	if err != nil {
		return fmt.Errorf("encode old product: %w", err)
	}
	after, err := json.Marshal(p)
	if err != nil {
		return fmt.Errorf("encode new product: %w", err)
	}
	_, err = tx.ExecContext(ctx, `INSERT INTO sync_changes(job_id,batch_index,supplier_id,product_id,revision,change_type,old_record_json,new_record_json,created_at) VALUES(?,?,?,?,?,?,?,?,?)`, e.JobID, e.BatchIndex, p.SupplierID, p.ProductID, p.Revision, e.Kind, string(before), string(after), p.UpdatedAt)
	if err != nil {
		return fmt.Errorf("write change: %w", err)
	}
	return nil
}
