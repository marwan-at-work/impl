package partier

import (
	"io"

	"marwan.io/impl/test_data/crowd"
	"marwan.io/impl/test_data/models"
)

// Partier defines a partier interface
type Partier interface {
	Singer
	io.ReadCloser
	io.WriteCloser
	Drink(models.Beverage) error
	BrowsePartyThemes(themes map[models.Theme]struct{}) error
	FavoritePerson() *models.Person
	SendBeverage(chan models.Beverage)
	GoWith(p *models.Person) (err error)
	Fight(reason string) []*Problem
	Hammered(interface {
		DrinkMore(interface {
			Singer
			Fight(reason string) []*Problem
		}) Partier
	}) Partier
}

// Singer sings
type Singer interface {
	Sing(c *crowd.Crowd) error
}

// Problem type
type Problem struct {
	Name string
}
