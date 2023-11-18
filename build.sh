#!/bin/sh

mkdir "Releases"

# 【darwin/amd64】
echo "Start build darwin/amd64 ..."
CGO_ENABLED=0 GOOS=darwin GOARCH=amd64 go build  -o ./Releases/m3u8-downloader-darwin-amd64 cmd/main.go

# 【linux/amd64】
echo "Start build linux/amd64 ..."
CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build  -o ./Releases/m3u8-downloader-linux-amd64 cmd/main.go

# 【windows/amd64】
echo "Start build windows/amd64 ..."
CGO_ENABLED=0 GOOS=windows GOARCH=amd64 go build  -o ./Releases/m3u8-downloader-windows-amd64.exe cmd/main.go

echo "Done!"
