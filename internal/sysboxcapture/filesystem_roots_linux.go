//go:build linux

package sysboxcapture

import "context"

// Each root is independent, but the caller owns the final generation barrier
// and publication. An error must settle all root work before returning.
func visitFilesystemRoots(ctx context.Context, count int, visit func(context.Context, int) error) error {
	for index := range count {
		if err := ctx.Err(); err != nil {
			return err
		}
		if err := visit(ctx, index); err != nil {
			return err
		}
	}
	return ctx.Err()
}
