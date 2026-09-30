package catalog

import (
	"context"
	"log"
	"time"
)

// RunPeriodically calls Service.RefreshAll on a ticker until ctx is
// cancelled, implementing the "periodic reconciliation scan" fallback from
// plan/ARCHITECTURE.md's Catalog refresh locked decision: "Fallback 1:
// periodic reconciliation scan (catches missed/failed webhook deliveries --
// GitHub doesn't guarantee delivery)."
//
// A single reconciliation pass failing outright (e.g. ListTags erroring) or
// returning per-module issues never crashes the process -- both are logged
// and the loop simply continues to the next tick, matching the "flagged,
// not fatal" pattern used throughout the catalog edge cases. Callers (WS-G)
// are expected to run this in its own goroutine, e.g.:
//
//	go catalogService.RunPeriodically(ctx, time.Hour)
func (s *Service) RunPeriodically(ctx context.Context, interval time.Duration) {
	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			issues, err := s.RefreshAll(ctx)
			if err != nil {
				log.Printf("catalog: reconciliation scan failed: %v", err)
				continue
			}
			for _, issue := range issues {
				log.Printf("catalog: reconciliation flagged module %q: %s", issue.ModuleName, issue.Reason)
			}
		}
	}
}
