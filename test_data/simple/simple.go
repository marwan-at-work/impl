package simple

import "marwan.io/impl/test_data/models"

// Interface to be implemented
type Interface interface {
	Drink(models.Beverage) error
}
