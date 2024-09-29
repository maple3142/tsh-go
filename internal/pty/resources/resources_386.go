//go:build windows && 386
// +build windows,386

package resources

import _ "embed"

//go:embed 386/winpty-agent.exe.gz
var WinptyAgent []byte

//go:embed 386/winpty-agent.exe.sha256
var WinptyAgentSha256 string

//go:embed 386/winpty.dll.gz
var WinptyDll []byte

//go:embed 386/winpty.dll.sha256
var WinptyDllSha256 string
