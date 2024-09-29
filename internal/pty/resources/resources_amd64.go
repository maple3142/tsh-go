//go:build windows && amd64
// +build windows,amd64

package resources

import _ "embed"

//go:embed amd64/winpty-agent.exe.gz
var WinptyAgent []byte

//go:embed amd64/winpty-agent.exe.sha256
var WinptyAgentSha256 string

//go:embed amd64/winpty.dll.gz
var WinptyDll []byte

//go:embed amd64/winpty.dll.sha256
var WinptyDllSha256 string
