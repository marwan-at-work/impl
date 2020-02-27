package dotter

import . "marwan.io/impl/test_data/models"

// Interface uses a dot import type
type Interface interface {
	Drink(Beverage) error
}
