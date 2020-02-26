package models

// Person is a human being
type Person struct {
	Name string
}

// Beverage is an enum
type Beverage int

// Beverage constants
const (
	Water = iota
	Soda
	Alcohol
)

// Theme is theme party after a person
type Theme = Person
