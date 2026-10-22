package domain_test

import (
	"catalogsync/internal/domain"
	"testing"
)

func TestParseRejectsInvalidEvents(t *testing.T) {
	for _, body := range []string{
		`{}`, `{"product_id":"P","revision":0,"status":"withdrawn"}`,
		`{"product_id":"P","revision":1,"status":"active","name":"x"}`,
		`{"product_id":"P","revision":1,"status":"active","name":"x","price_cents":-1}`,
		`{"product_id":"P","revision":1,"status":"withdrawn","name":null}`,
		`{"product_id":"P","revision":1,"status":"withdrawn","price_cents":0}`,
		`{"product_id":"P","revision":1,"status":"unknown"}`,
	} {
		t.Run(body, func(t *testing.T) {
			// When
			_, err := domain.ParseEvent([]byte(body))
			// Then
			if err == nil {
				t.Fatal("accepted invalid event")
			}
		})
	}
}

func TestCanonicalDigestIgnoresJSONFieldOrder(t *testing.T) {
	// Given
	a, err := domain.ParseEvent([]byte(`{"product_id":"P","revision":1,"status":"active","name":"Mouse","price_cents":0}`))
	if err != nil {
		t.Fatal(err)
	}
	// When
	b, err := domain.ParseEvent([]byte(`{"price_cents":0,"name":"Mouse","status":"active","revision":1,"product_id":"P"}`))
	if err != nil {
		t.Fatal(err)
	}
	// Then
	if a.Digest() != b.Digest() {
		t.Fatal("digest changed with field order")
	}
}
