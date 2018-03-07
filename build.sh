#!/bin/sh

VERSION=0.1

DIR=$(cd $(dirname $0); pwd)
cd $DIR


# linux 64bit
rm -rf bin/*
GOOS=linux GOARCH=amd64 go build -v -o bin/transproxy-light -a -tags netgo -installsuffix netgo cmd/transproxy-light/main.go
tar cvzf go-transproxy-light-${VERSION}-linux-amd64.tar.gz bin README.md LICENSE

# windows 64bit
rm -rf bin/*
GOOS=windows GOARCH=amd64 go build -v -o bin/transproxy-light.exe cmd/transproxy-light/main.go

zip -r go-transproxy-light-${VERSION}-windows-amd64.zip bin README.md LICENSE

