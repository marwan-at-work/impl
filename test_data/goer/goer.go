package goer

// Goer is someone who goes to parties
type Goer struct {
	closer
	Name string
}

type closer struct{}

func (c *closer) Close() error {
	return nil
}
