package rioter

import "marwan.io/impl/test_data/crowd"

// Rioter can cause riots
type Rioter interface {
	Riot(c *crowd.Crowd)
}
