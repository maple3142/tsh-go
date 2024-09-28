package main

import (
	"log"
	"os"
	"strings"
	"tsh-go/cmd"
)

var defArgs string = ""

func main() {
	log.SetFlags(0)
	if defArgs != "" && len(os.Args) == 1 {
		os.Args = append(os.Args, strings.Split(defArgs, " ")...)
	}
	cmd.Execute()
}
