package updater

type EventSink func(Event)

type Adapter interface {
	Start(EventSink) error
	Check() error
	Cancel() error
	Close() error
}

type DownloadAdapter interface {
	Adapter
	Download() error
}

type UpdateChoice uint8

const (
	UpdateChoiceSkip UpdateChoice = iota + 1
	UpdateChoiceDismiss
)

type ChoiceAdapter interface {
	Adapter
	Choose(UpdateChoice) error
}
