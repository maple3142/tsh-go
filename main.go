package main

import (
	"log"
	"os"
	"tsh-go/cmd"
)

var variant string = "tsh"

func main() {
	log.SetFlags(0)
	if variant == "tshd" && len(os.Args) == 1 {
		// if this is tshd and no arguments passed, run as daemon with default configurations
		os.Args = append(os.Args, "server", "--daemon")
	}
	cmd.Execute()
}
