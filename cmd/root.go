package cmd

import (
	_ "embed"
	"fmt"
	"io"
	"log"
	"os"

	"github.com/spf13/cobra"
)

//go:embed secret.txt
var defaultSecret string
var defaultPort = 2413

var quiet bool

func init() {
	// rootCmd.PersistentFlags().BoolP("help", "", false, "help for this command") // disable `-h` flag
	rootCmd.PersistentFlags().BoolVarP(&quiet, "quiet", "q", false, "Quiet mode")
	rootCmd.Root().CompletionOptions.HiddenDefaultCmd = true // hide completion command
}

func enableQuietMode() {
	log.SetOutput(io.Discard)
}

var rootCmd = &cobra.Command{
	Use:   "tsh",
	Short: "Tiny SHell written in Go",
	Long:  `This is Tiny SHell rewritten in Go programming language.`,
	PersistentPreRun: func(cmd *cobra.Command, args []string) {
		if quiet {
			enableQuietMode()
		}
	},
}

func Execute() {
	if err := rootCmd.Execute(); err != nil {
		fmt.Println(err)
		os.Exit(1)
	}
}
