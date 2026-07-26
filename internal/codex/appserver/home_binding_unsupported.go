//go:build !darwin

package appserver

import "context"

func openConfirmedHomeBinding(
	context.Context,
	ConfirmedHome,
) (processHomeBinding, error) {
	return nil, ErrConfirmedHomeChanged
}
