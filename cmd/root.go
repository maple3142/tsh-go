package cmd

import (
	"fmt"
	"os"

	"github.com/spf13/cobra"
)

func init() {
	rootCmd.PersistentFlags().BoolP("help", "", false, "help for this command") // disable `-h` flag
	rootCmd.Root().CompletionOptions.HiddenDefaultCmd = true                    // hide completion command
}

var rootCmd = &cobra.Command{
	Use:   "tsh",
	Short: "Tiny SHell written in Go",
	Long:  `This is Tiny SHell rewritten in Go programming language.`,
}

func Execute() {
	if err := rootCmd.Execute(); err != nil {
		fmt.Println(err)
		os.Exit(1)
	}
}
