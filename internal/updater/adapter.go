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
