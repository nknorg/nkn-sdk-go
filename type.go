package nkn

import (
	"github.com/nknorg/nkngomobile"
)

// NewStringArray creates a StringArray from a list of string elements.
var NewStringArray = nkngomobile.NewStringArray

// NewStringArrayFromString creates a StringArray from a single string input.
// The input string will be split to string array by whitespace.
var NewStringArrayFromString = nkngomobile.NewStringArrayFromString

// NewStringMap creates a StringMap from a map.
var NewStringMap = nkngomobile.NewStringMap

// NewStringMapWithSize creates an empty StringMap with a given size.
var NewStringMapWithSize = nkngomobile.NewStringMapWithSize

type ResolverInterface interface {
	Resolve(address string) (string, error)
}
