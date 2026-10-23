//go:build !linux

package sidecar

import "github.com/mhsanaei/3x-ui/v3/internal/database/model"

// EnsureRoutingForInbounds is a no-op outside Linux: the sidecar binaries and
// the policy-routing tools are Linux-only.
func EnsureRoutingForInbounds(_ []*model.Inbound) {}

// EnsureRouting is a no-op outside Linux.
func EnsureRouting(_ Instance) {}

// ClearRouting is a no-op outside Linux.
func ClearRouting(_ Instance) {}
