package plugin

type Plugin interface {
	Name() string
	Register(server any)
}
