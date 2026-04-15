package terminal

type PTYRunner interface {
	Alive(name string) bool
	Resize(name string, rows, cols uint16) error
	Subscribe(name string) ([]byte, <-chan []byte, func(), error)
	WriteInput(name string, data []byte) (int, error)
	Sessions() []string
	Start(name, cwd string, argv []string, logPath string, rows, cols uint16) error
}
