package main

import (
	"crypto/rand"
	"fmt"
)

func main() {
	key := make([]byte, 16)
	rand.Read(key)
	fmt.Printf("%x", key)
}
