package wsx

import "context"

func validateContext(ctx context.Context) error {
	if ctx == nil {
		return ErrContextRequired
	}
	return nil
}
