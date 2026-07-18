//go:build !darwin || !cgo

package updater

type NativeError struct {
	Code    FaultCode
	Message string
}

func (err *NativeError) Error() string { return "sparkle " + string(err.Code) + ": " + err.Message }

type SparkleAdapter struct{}

func NewSparkleAdapter() *SparkleAdapter { return &SparkleAdapter{} }
func (*SparkleAdapter) Start(EventSink) error {
	return &NativeError{Code: FaultUnavailable, Message: "Sparkle requires macOS with cgo"}
}
func (*SparkleAdapter) Check() error    { return ErrNotStarted }
func (*SparkleAdapter) Download() error { return ErrCannotDownload }
func (*SparkleAdapter) Install() error  { return ErrCannotInstall }
func (*SparkleAdapter) Cancel() error   { return ErrCannotCancel }
func (*SparkleAdapter) Close() error    { return nil }
