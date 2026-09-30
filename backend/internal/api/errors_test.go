package api

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"

	"tf-automation/backend/internal/data"
	"tf-automation/backend/internal/naming"
)

func TestWriteErr_Mapping(t *testing.T) {
	cases := []struct {
		name string
		err  error
		want int
	}{
		{"data.ErrNotFound", data.ErrNotFound, http.StatusNotFound},
		{"data.ErrStaleWrite", data.ErrStaleWrite, http.StatusConflict},
		{"data.ConflictError", &data.ConflictError{Constraint: "x", Detail: "y"}, http.StatusConflict},
		{"data.ErrCounterExhausted", data.ErrCounterExhausted, http.StatusBadRequest},
		{"naming.ErrCustomerShortCodeMissing", naming.ErrCustomerShortCodeMissing, http.StatusBadRequest},
		{"api badRequest", badRequest("bad input"), http.StatusBadRequest},
		{"api notFound", notFound("missing"), http.StatusNotFound},
		{"api conflictErr", conflictErr("nope"), http.StatusConflict},
		{"validation error", asValidation(fmt.Errorf("codegen: missing required value")), http.StatusBadRequest},
		{"generic error", fmt.Errorf("boom"), http.StatusInternalServerError},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			rec := httptest.NewRecorder()
			writeErr(rec, c.err)
			if rec.Code != c.want {
				t.Errorf("writeErr(%v): got status %d, want %d", c.err, rec.Code, c.want)
			}
		})
	}
}
