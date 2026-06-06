package cmd

import (
	_ "embed"
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
	rootCmd.PersistentFlags().BoolVarP(&quiet, "quiet", "q", false, "Quiet mode")
	rootCmd.Root().CompletionOptions.HiddenDefaultCmd = true // hide completion command
}

func enableQuietMode() {
	log.SetOutput(io.Discard)
}

var rootCmd = &cobra.Command{
	Use:           "tsh",
	Short:         "TSH",
	Long:          `TSH is like SSH, but simpler.`,
	SilenceUsage:  true,
	SilenceErrors: true,
	PersistentPreRun: func(cmd *cobra.Command, args []string) {
		if quiet {
			enableQuietMode()
		}
	},
}

func Execute() {
	if err := rootCmd.Execute(); err != nil {
		log.Println(err)
		os.Exit(1)
	}
}
