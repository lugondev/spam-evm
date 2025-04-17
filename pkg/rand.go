package pkg

import (
	"math/rand"
	"time"
)

var r *rand.Rand

func init() {
	r = rand.New(rand.NewSource(time.Now().UnixNano()))
}

// RandomInt returns a random integer between min and max inclusive
func RandomInt(min, max int) int {
	return r.Intn(max-min+1) + min
}
