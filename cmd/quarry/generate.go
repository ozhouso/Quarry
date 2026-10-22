package main

// These make the Properties > Details tab on quarry.exe. The generated .syso files are
// committed on purpose, so a plain `go build` picks them up for anyone, no extra tools.
// After editing versioninfo.json run `go generate ./cmd/quarry` to refresh them.

//go:generate go run github.com/josephspurrier/goversioninfo/cmd/goversioninfo@v1.4.1 -64 -o resource_windows_amd64.syso versioninfo.json
//go:generate go run github.com/josephspurrier/goversioninfo/cmd/goversioninfo@v1.4.1 -arm -64 -o resource_windows_arm64.syso versioninfo.json
