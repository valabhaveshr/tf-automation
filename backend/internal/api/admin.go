package api

import (
	"errors"
	"net/http"

	"tf-automation/backend/internal/data"
)

// handleListCustomers implements GET /api/admin/customers.
func (s *Server) handleListCustomers(w http.ResponseWriter, r *http.Request) {
	customers, err := s.Store.ListCustomers(r.Context())
	if err != nil {
		writeErr(w, err)
		return
	}
	out := make([]customerResponse, 0, len(customers))
	for _, c := range customers {
		out = append(out, customerResponse{ID: c.ID, Name: c.Name, BusinessShortCode: c.BusinessShortCode})
	}
	writeJSON(w, http.StatusOK, out)
}

type createCustomerRequest struct {
	Name              string `json:"name"`
	BusinessShortCode string `json:"business_short_code"`
}

// handleCreateCustomer implements POST /api/admin/customers, delegating
// business_short_code format validation to naming.Engine.CreateCustomer
// (WS-I), which in turn persists via WS-A's Store.CreateCustomer.
func (s *Server) handleCreateCustomer(w http.ResponseWriter, r *http.Request) {
	var req createCustomerRequest
	if err := decodeJSON(r, &req); err != nil {
		writeErr(w, badRequest("invalid request body"))
		return
	}
	if req.Name == "" {
		writeErr(w, badRequest("name is required"))
		return
	}

	c, err := s.Naming.CreateCustomer(r.Context(), req.Name, req.BusinessShortCode)
	if err != nil {
		// naming.Engine.CreateCustomer returns a plain validation error for
		// a malformed business_short_code (not a data.* sentinel) -- map it
		// to 400; a duplicate name comes back wrapping *data.ConflictError
		// from the underlying store and is mapped to 409 by writeErr as
		// usual via errors.As.
		var conflict *data.ConflictError
		if errors.As(err, &conflict) {
			writeErr(w, err)
			return
		}
		writeErr(w, badRequest(err.Error()))
		return
	}

	s.logAudit(r, "customer.create", "customer", strPtr(c.ID.String()), map[string]string{
		"name":                c.Name,
		"business_short_code": c.BusinessShortCode,
	})

	writeJSON(w, http.StatusCreated, customerResponse{ID: c.ID, Name: c.Name, BusinessShortCode: c.BusinessShortCode})
}

// handleListNamingTemplates implements GET /api/admin/naming-templates.
func (s *Server) handleListNamingTemplates(w http.ResponseWriter, r *http.Request) {
	templates, err := s.Store.ListNamingTemplates(r.Context())
	if err != nil {
		writeErr(w, err)
		return
	}
	out := make([]namingTemplateResponse, 0, len(templates))
	for _, t := range templates {
		out = append(out, namingTemplateResponse{
			Cloud: t.Cloud, ResourceType: t.ResourceType, Template: t.Template, NumIDWidth: t.NumIDWidth,
		})
	}
	writeJSON(w, http.StatusOK, out)
}

// handleSaveNamingTemplate implements POST /api/admin/naming-templates
// (create-or-update, per the OpenAPI summary), delegating token validation
// to naming.Engine.SaveNamingTemplate (WS-I), which rejects an unknown
// {token} at save time per the locked decision.
func (s *Server) handleSaveNamingTemplate(w http.ResponseWriter, r *http.Request) {
	var req namingTemplateRequest
	if err := decodeJSON(r, &req); err != nil {
		writeErr(w, badRequest("invalid request body"))
		return
	}
	if req.Cloud == "" || req.ResourceType == "" || req.Template == "" {
		writeErr(w, badRequest("cloud, resource_type, and template are required"))
		return
	}
	width := req.NumIDWidth
	if width <= 0 {
		width = 2
	}

	saved, err := s.Naming.SaveNamingTemplate(r.Context(), data.NamingTemplate{
		Cloud:        req.Cloud,
		ResourceType: req.ResourceType,
		Template:     req.Template,
		NumIDWidth:   width,
		CreatedBy:    currentUsername(r),
	})
	if err != nil {
		// ValidateTemplate's "unknown token" error is a plain error, not a
		// data.* sentinel -> 400 per contracts/openapi.yaml.
		writeErr(w, badRequest(err.Error()))
		return
	}

	// NamingTemplate has no synthetic id column (its natural key is
	// cloud+resource_type) -- use that as the audit entry's resource_id.
	s.logAudit(r, "naming_template.save", "naming_template", strPtr(saved.Cloud+"/"+saved.ResourceType), map[string]string{
		"cloud":         saved.Cloud,
		"resource_type": saved.ResourceType,
	})

	writeJSON(w, http.StatusOK, namingTemplateResponse{
		Cloud: saved.Cloud, ResourceType: saved.ResourceType, Template: saved.Template, NumIDWidth: saved.NumIDWidth,
	})
}
