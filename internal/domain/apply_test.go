package domain_test

import (
	"errors"
	"testing"

	"catalogsync/internal/domain"
)

func parsed(t *testing.T, raw string) domain.Event {
	t.Helper()
	e, err := domain.ParseEvent([]byte(raw))
	if err != nil {
		t.Fatal(err)
	}
	return e
}

func TestDecideRevisionAndTombstoneRules(t *testing.T) {
	// Given
	active := parsed(t, `{"product_id":"P","revision":3,"status":"active","name":"Mouse","price_cents":100}`)
	withdrawal := parsed(t, `{"product_id":"P","revision":5,"status":"withdrawn"}`)
	base, err := domain.Decide(nil, active)
	if err != nil {
		t.Fatal(err)
	}
	tombstone, err := domain.Decide(&base.Product, withdrawal)
	if err != nil {
		t.Fatal(err)
	}
	tests := []struct {
		name     string
		old      *domain.Product
		raw      string
		kind     domain.Kind
		conflict bool
	}{
		{"create", nil, `{"product_id":"P","revision":1,"status":"active","name":"Mouse","price_cents":0}`, domain.Created, false},
		{"unknown withdrawal", nil, `{"product_id":"P","revision":5,"status":"withdrawn"}`, domain.Withdrew, false},
		{"older active", &base.Product, `{"product_id":"P","revision":1,"status":"active","name":"Old","price_cents":0}`, domain.Stale, false},
		{"duplicate", &base.Product, `{"product_id":"P","revision":3,"status":"active","name":"Mouse","price_cents":100}`, domain.Duplicate, false},
		{"conflict price", &base.Product, `{"product_id":"P","revision":3,"status":"active","name":"Mouse","price_cents":101}`, "", true},
		{"conflict status", &tombstone.Product, `{"product_id":"P","revision":5,"status":"active","name":"Mouse","price_cents":100}`, "", true},
		{"higher identical fields", &base.Product, `{"product_id":"P","revision":4,"status":"active","name":"Mouse","price_cents":100}`, domain.Updated, false},
		{"withdraw", &base.Product, `{"product_id":"P","revision":5,"status":"withdrawn"}`, domain.Withdrew, false},
		{"stale resurrection", &tombstone.Product, `{"product_id":"P","revision":3,"status":"active","name":"Mouse","price_cents":100}`, domain.Stale, false},
		{"reactivate", &tombstone.Product, `{"product_id":"P","revision":6,"status":"active","name":"Mouse Pro","price_cents":200}`, domain.Reactivated, false},
		{"duplicate withdrawn keeps display", &tombstone.Product, `{"product_id":"P","revision":5,"status":"withdrawn"}`, domain.Duplicate, false},
		{"newer tombstone", &tombstone.Product, `{"product_id":"P","revision":7,"status":"withdrawn"}`, domain.Withdrew, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			event := parsed(t, tt.raw)
			// When
			got, err := domain.Decide(tt.old, event)
			// Then
			if tt.conflict {
				if !errors.Is(err, domain.ErrConflict) {
					t.Fatalf("want conflict, got %v", err)
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			if got.Kind != tt.kind {
				t.Fatalf("kind=%s want=%s", got.Kind, tt.kind)
			}
			if tt.old != nil && got.Product.Revision < tt.old.Revision {
				t.Fatal("revision decreased")
			}
			if tt.kind == domain.Withdrew && tt.old != nil && *got.Product.Name != *tt.old.Name {
				t.Fatal("lost tombstone display fields")
			}
			if tt.old == nil && tt.kind == domain.Withdrew && (got.Product.Name != nil || got.Product.PriceCents != nil) {
				t.Fatal("unknown tombstone has metadata")
			}
		})
	}
}
