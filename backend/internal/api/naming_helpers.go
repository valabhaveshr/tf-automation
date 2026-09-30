package api

import (
	"context"
	"errors"

	"github.com/google/uuid"

	"tf-automation/backend/internal/catalog"
	"tf-automation/backend/internal/data"
	"tf-automation/backend/internal/naming"
)

type namingMode int

const (
	namingModeSuggest namingMode = iota
	namingModeAllocate
)

// resolvedCustomer bundles what naming lookups need about the record's
// customer, resolved once per request (not once per module selection).
type resolvedCustomer struct {
	id                uuid.UUID
	businessShortCode string
	found             bool
}

// resolveCustomer looks up req.customer_name in the customers registry.
// Per plan/ARCHITECTURE.md, records.customer_id is nullable for
// "pre-registry rows" -- a record for a customer that was never registered
// (business_short_code never set) is perfectly valid UNLESS one of its
// selected modules opts into naming, in which case applyNaming blocks with
// the documented error. A genuine data.Store error (not ErrNotFound) is
// propagated so it surfaces as a 500.
func (s *Server) resolveCustomer(ctx context.Context, customerName string) (resolvedCustomer, error) {
	c, err := s.Store.GetCustomerByName(ctx, customerName)
	if err != nil {
		if errors.Is(err, data.ErrNotFound) {
			return resolvedCustomer{}, nil
		}
		return resolvedCustomer{}, err
	}
	return resolvedCustomer{id: c.ID, businessShortCode: c.BusinessShortCode, found: true}, nil
}

// applyNaming resolves suggested/allocated names for every instance of one
// naming-enabled module selection, merging each into sel.Instances IN
// PLACE wherever the target variable is currently empty/missing -- per the
// task brief: "Merge suggested names into instance values where the target
// variable is empty." Modules that don't declare naming_resource_type are
// a complete no-op.
//
// mode selects Suggest (tentative, non-allocating -- preview paths only)
// vs Allocate (row-locked, counter-consuming -- create/update save paths
// only). Per WS-I's design and the locked decision ("allocation happens
// atomically at save time, not preview"), Allocate must never be called
// from a preview handler.
//
// The UserLabel token fed to the naming template is the instance's own map
// key (e.g. "web-01") -- the one stable, always-present, user-chosen
// per-instance identifier available at this layer. This is an inferred
// choice, not spelled out in contracts/openapi.yaml or
// plan/ARCHITECTURE.md; flagged in the task report.
func (s *Server) applyNaming(ctx context.Context, mode namingMode, envName, projectName string, cust resolvedCustomer, sel *moduleInstanceInput, meta catalog.ModuleMetadata) (suggestedNames map[string]string, warnings []string, err error) {
	if meta.NamingResourceType == "" {
		return nil, nil, nil
	}

	// Per the "Customer has no business_short_code set yet" edge case:
	// blocked BEFORE the naming engine is invoked further. Checked here
	// (rather than solely relying on naming.Engine's own error, which only
	// distinguishes "found customer with blank code" from a generic lookup
	// failure) so a customer that was never registered at all gets the
	// same clear message.
	if !cust.found || cust.businessShortCode == "" {
		return nil, nil, badRequest("this customer needs a business short code configured (see /api/admin/customers)")
	}

	targetVar := meta.NamingTargetVariable
	if targetVar == "" {
		targetVar = meta.InstanceLabelField
	}
	if targetVar == "" {
		// Module opted into naming_resource_type but declares no target/
		// instance_label_field to receive the computed name -- a module-
		// authoring gap, not a user error; no-op rather than fail the save.
		return nil, nil, nil
	}

	suggestedNames = make(map[string]string, len(sel.Instances))

	instanceNames := make([]string, 0, len(sel.Instances))
	for name := range sel.Instances {
		instanceNames = append(instanceNames, name)
	}

	for _, instName := range instanceNames {
		if sel.Instances[instName] == nil {
			sel.Instances[instName] = map[string]any{}
		}

		// Only compute (and, in Allocate mode, only ever CONSUME A COUNTER
		// SLOT for) instances whose target variable is still empty. This
		// matters most for Allocate: re-saving an already-named instance
		// on every edit must NOT burn a fresh num_id each time -- the
		// instance already carries its previously-allocated name, and per
		// the locked decision num_id counters are monotonic/never reused,
		// so wasting one on every routine edit would be a real bug, not
		// just a cosmetic one.
		if !isEmptyValue(sel.Instances[instName][targetVar]) {
			continue
		}

		req := naming.NameRequest{
			Cloud:        meta.Cloud,
			ResourceType: meta.NamingResourceType,
			EnvName:      envName,
			Project:      projectName,
			UserLabel:    instName,
			CustomerID:   cust.id,
		}

		var name string
		var nameErr error
		if mode == namingModeAllocate {
			name, _, nameErr = s.Naming.Allocate(ctx, req)
		} else {
			name, nameErr = s.Naming.Suggest(ctx, req)
		}

		if nameErr != nil {
			switch {
			case errors.Is(nameErr, naming.ErrNoTemplateConfigured):
				// NOT a hard failure per the edge case table: fall back to
				// a plain editable name field (i.e. don't touch the
				// instance's value), surfaced as a response warning.
				warnings = append(warnings, "no naming template configured for resource type \""+meta.NamingResourceType+"\"; using a plain editable name field")
				continue
			case errors.Is(nameErr, naming.ErrCustomerShortCodeMissing):
				return nil, nil, badRequest("this customer needs a business short code configured (see /api/admin/customers)")
			case errors.Is(nameErr, data.ErrCounterExhausted):
				return nil, nil, badRequest(nameErr.Error())
			default:
				return nil, nil, nameErr
			}
		}

		suggestedNames[sel.ModuleName+"."+instName] = name
		sel.Instances[instName][targetVar] = name
	}

	return suggestedNames, warnings, nil
}

func isEmptyValue(v any) bool {
	if v == nil {
		return true
	}
	if str, ok := v.(string); ok {
		return str == ""
	}
	return false
}
