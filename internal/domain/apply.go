package domain

import "fmt"

type Kind string

const (
	Created     Kind = "created"
	Updated     Kind = "updated"
	Withdrew    Kind = "withdrawn"
	Reactivated Kind = "reactivated"
	Duplicate   Kind = "duplicate"
	Stale       Kind = "stale"
)

type Decision struct {
	Kind    Kind
	Product Product
}

type Counts struct {
	Created     int64 `json:"created"`
	Updated     int64 `json:"updated"`
	Withdrawn   int64 `json:"withdrawn"`
	Reactivated int64 `json:"reactivated"`
	Duplicate   int64 `json:"duplicate"`
	Stale       int64 `json:"stale"`
}

func (c *Counts) Add(kind Kind) error {
	switch kind {
	case Created:
		c.Created++
	case Updated:
		c.Updated++
	case Withdrew:
		c.Withdrawn++
	case Reactivated:
		c.Reactivated++
	case Duplicate:
		c.Duplicate++
	case Stale:
		c.Stale++
	default:
		return fmt.Errorf("unknown decision %q", kind)
	}
	return nil
}

func Decide(existing *Product, event Event) (Decision, error) {
	e := event.data
	if existing != nil {
		switch {
		case e.Revision < existing.Revision:
			return Decision{Kind: Stale, Product: *existing}, nil
		case e.Revision == existing.Revision:
			if event.digest != existing.EventDigest {
				return Decision{}, &RecordError{ProductID: e.ProductID, Cause: ErrConflict}
			}
			return Decision{Kind: Duplicate, Product: *existing}, nil
		}
	}
	next := Product{ProductID: e.ProductID, Revision: e.Revision, Status: e.Status, Name: e.Name, PriceCents: e.PriceCents, EventDigest: event.digest}
	switch e.Status {
	case Withdrawn:
		if existing != nil {
			next.Name = existing.Name
			next.PriceCents = existing.PriceCents
			next.SupplierID = existing.SupplierID
		}
		return Decision{Kind: Withdrew, Product: next}, nil
	case Active:
		if existing == nil {
			return Decision{Kind: Created, Product: next}, nil
		}
		next.SupplierID = existing.SupplierID
		if existing.Status == Withdrawn {
			return Decision{Kind: Reactivated, Product: next}, nil
		}
		return Decision{Kind: Updated, Product: next}, nil
	default:
		return Decision{}, fmt.Errorf("%w: unparsed event", ErrInvalid)
	}
}
