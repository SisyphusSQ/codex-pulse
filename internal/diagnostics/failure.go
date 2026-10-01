package diagnostics

import (
	"errors"
	"strconv"
)

// Failure 保留错误链，但 Error 和诊断字段只暴露固定分类。
type Failure struct {
	Stage, Reason string
	RPCCode       *int64
	ExitCode      *int
	Cause         error
}

func (failure *Failure) Error() string {
	message := "refresh " + failure.Stage + ": " + failure.Reason
	if failure.RPCCode != nil {
		return "App Server RPC error " + strconv.FormatInt(*failure.RPCCode, 10)
	}
	return message
}
func (failure *Failure) Unwrap() error { return failure.Cause }

// Wrap 为 operation boundary 添加不含外部正文的诊断上下文。
func Wrap(err error, stage, reason string) error {
	if err == nil {
		return nil
	}
	event := safeEvent(Event{Stage: stage, Reason: reason})
	if failure, ok := errors.AsType[*Failure](err); ok {
		// 已知的底层起点优先于高层 operation boundary。
		return &Failure{Stage: failure.Stage, Reason: failure.Reason, RPCCode: failure.RPCCode, ExitCode: failure.ExitCode, Cause: err}
	}
	return &Failure{Stage: event.Stage, Reason: event.Reason, Cause: err}
}

// FromError 优先保留最初的带类型错误，避免高层通用不可用覆盖故障起点。
func FromError(err error, stage, reason string) Event {
	event := Event{Stage: stage, Outcome: "failed", Reason: reason}
	if failure, ok := errors.AsType[*Failure](err); ok {
		event.Stage = failure.Stage
		event.Reason = failure.Reason
		event.RPCCode = failure.RPCCode
		event.ExitCode = failure.ExitCode
	}
	return event
}
