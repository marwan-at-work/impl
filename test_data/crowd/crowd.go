package crowd

import "marwan.io/impl/test_data/models"

// Crowd represents a number of people
type Crowd struct {
	Mood   string
	People []*models.Person
}
