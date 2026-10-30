//go:build !linux

package sidecar

import "github.com/mhsanaei/3x-ui/v3/internal/database/model"

// EnsureRoutingForInbounds is a no-op outside Linux: the cores and the routing
// tools do not exist there, yet callers invoke it unconditionally.
func EnsureRoutingForInbounds(_ []*model.Inbound) {}

// EnsureRouting is a no-op outside Linux.
func EnsureRouting(_ Instance) {}

// ClearRouting is a no-op outside Linux.
func ClearRouting(_ Instance) {}
