package module

type Provider func() (any, error)

type Module struct {
	Name      string
	Providers map[string]Provider
}

func New(name string) *Module                      { return &Module{Name: name, Providers: map[string]Provider{}} }
func (m *Module) Register(name string, p Provider) { m.Providers[name] = p }
