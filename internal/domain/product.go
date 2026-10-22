package domain

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"strings"
)

type Status string

const (
	Active    Status = "active"
	Withdrawn Status = "withdrawn"
)

var ErrInvalid = errors.New("invalid product event")
var ErrConflict = errors.New("same revision has different content")

type Product struct {
	SupplierID  string  `json:"supplier_id"`
	ProductID   string  `json:"product_id"`
	Revision    int64   `json:"revision"`
	Status      Status  `json:"status"`
	Name        *string `json:"name"`
	PriceCents  *int64  `json:"price_cents"`
	EventDigest string  `json:"event_digest"`
	UpdatedAt   string  `json:"updated_at"`
}

type eventData struct {
	ProductID  string  `json:"product_id"`
	Revision   int64   `json:"revision"`
	Status     Status  `json:"status"`
	Name       *string `json:"name"`
	PriceCents *int64  `json:"price_cents"`
}

// Event is constructed only by ParseEvent; its payload is immutable to callers.
type Event struct {
	data   eventData
	digest string
}

func (e Event) Digest() string    { return e.digest }
func (e Event) ProductID() string { return e.data.ProductID }

type RecordError struct {
	ProductID string
	Cause     error
}

func (e *RecordError) Error() string { return fmt.Sprintf("product %q: %v", e.ProductID, e.Cause) }
func (e *RecordError) Unwrap() error { return e.Cause }

func ParseEvent(raw []byte) (Event, error) {
	var wire struct {
		ProductID  string          `json:"product_id"`
		Revision   int64           `json:"revision"`
		Status     Status          `json:"status"`
		Name       json.RawMessage `json:"name"`
		PriceCents json.RawMessage `json:"price_cents"`
	}
	fail := func(err error) (Event, error) {
		return Event{}, &RecordError{ProductID: wire.ProductID, Cause: fmt.Errorf("%w: %w", ErrInvalid, err)}
	}
	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&wire); err != nil {
		return fail(err)
	}
	var extra json.RawMessage
	if err := dec.Decode(&extra); !errors.Is(err, io.EOF) {
		return fail(errors.New("trailing JSON"))
	}
	if strings.TrimSpace(wire.ProductID) == "" || wire.Revision <= 0 {
		return fail(errors.New("nonempty product_id and positive revision required"))
	}
	data := eventData{ProductID: wire.ProductID, Revision: wire.Revision, Status: wire.Status}
	switch wire.Status {
	case Active:
		if len(wire.Name) == 0 || len(wire.PriceCents) == 0 {
			return fail(errors.New("active requires name and integer price_cents"))
		}
		if err := json.Unmarshal(wire.Name, &data.Name); err != nil {
			return fail(err)
		}
		if err := json.Unmarshal(wire.PriceCents, &data.PriceCents); err != nil {
			return fail(err)
		}
		if data.Name == nil || strings.TrimSpace(*data.Name) == "" || data.PriceCents == nil || *data.PriceCents < 0 {
			return fail(errors.New("active requires nonempty name and nonnegative price_cents"))
		}
	case Withdrawn:
		if len(wire.Name) > 0 || len(wire.PriceCents) > 0 {
			return fail(errors.New("withdrawn must omit name and price_cents, including null"))
		}
	default:
		return fail(errors.New("status must be active or withdrawn"))
	}
	encoded, err := json.Marshal(data)
	if err != nil {
		return fail(err)
	}
	sum := sha256.Sum256(encoded)
	return Event{data: data, digest: hex.EncodeToString(sum[:])}, nil
}
