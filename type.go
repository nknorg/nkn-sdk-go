package nkn

import (
	"github.com/nknorg/nkngomobile"
)

// StringArray is a wrapper type for gomobile compatibility. StringArray is not
// protected by lock and should not be read and write at the same time.
type StringArray nkngomobile.IStringArray

// NewStringArray creates a StringArray from a list of string elements.
var NewStringArray = nkngomobile.NewStringArray

// NewStringArrayFromString creates a StringArray from a single string input.
// The input string will be split to string array by whitespace.
var NewStringArrayFromString = nkngomobile.NewStringArrayFromString

// StringMap is a wrapper type for gomobile compatibility. StringMap is not
// protected by lock and should not be read and write at the same time.
type StringMap nkngomobile.IStringMap

// StringMapFunc is a wrapper type for gomobile compatibility.
type StringMapFunc nkngomobile.IStringMapFunc

// NewStringMap creates a StringMap from a map.
var NewStringMap = nkngomobile.NewStringMap

// NewStringMapWithSize creates an empty StringMap with a given size.
var NewStringMapWithSize = nkngomobile.NewStringMapWithSize
