// Package entry provides platform-specific wrappers around the application's
// entry points to manage its lifecycle.
package entry

import (
	"context"
	"time"
)

// Timeout for shutting down gracefully.
//
// If the [InnerMain] function does not return within this time, the process
// may be shut down ungracefully.
const shutdownTimeout = 10 * time.Second

// InnerMain is an inner entry function responsible for creating a
// [client.Client] and managing its configuration and lifecycle. It is called by
// [RunMain] which handles platform-specific mechanics to manage the passed
// Context.
type InnerMain func(ctx context.Context) error
