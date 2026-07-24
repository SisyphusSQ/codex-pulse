package core

type serviceFailure struct {
	cause error
}

func newServiceFailure(cause error) error {
	if cause == nil {
		return nil
	}
	return &serviceFailure{cause: cause}
}

func (*serviceFailure) Error() string {
	return ErrQuery.Error()
}

func (failure *serviceFailure) Unwrap() error {
	if failure == nil {
		return nil
	}
	return failure.cause
}
