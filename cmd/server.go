package cmd

import (
	"tsh-go/internal/server"

	"github.com/spf13/cobra"
)

func init() {
	serverCmd.PersistentFlags().StringVarP(&serverSecret, "secret", "s", defaultSecret, "Pre-shared secret for encryption")
	serverCmd.PersistentFlags().StringVarP(&serverConnectBackHost, "connect", "c", "", "Connect-back host (If specified, server will try to connect to it)")
	serverCmd.PersistentFlags().IntVarP(&serverPort, "port", "p", defaultPort, "Target port")
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
	Use:     "server [-s secret] [-c connect] [-p port] [-d]",
	Aliases: []string{"s"},
	Args:    cobra.NoArgs,
	Short:   "TSH server",
	Long:    `TSH server listens for incoming connections or connect back to client, and does the specified action.`,
	Example: `  tsh server
  tsh server -s hello -p 1337
  tsh server -c 192.168.87.63 --delay 3 -d`,
	Run: func(cmd *cobra.Command, args []string) {
		server.Run([]byte(serverSecret), serverConnectBackHost, serverPort, serverConnectBackDelay, serverIsDaemon)
	},
}
