#!/bin/sh

VERSION=0.1

DIR=$(cd $(dirname $0); pwd)
cd $DIR


# linux 64bit
rm -rf bin/*
GOOS=linux GOARCH=amd64 go build -v -o bin/transproxy-light -a -tags netgo -installsuffix netgo cmd/transproxy-light/main.go

if [ "$?" -ne 0 ]; then
    echo "Build error"
    exit 1
fi

tar cvzf go-transproxy-light-${VERSION}-linux-amd64.tar.gz bin README.md LICENSE


# windows 32bit
rm -rf bin/*
rsrc -manifest transproxy-light.manifest -o rsrc.syso
GOOS=windows GOARCH=386 go build -v -o bin/transproxy-light.exe

if [ "$?" -ne 0 ]; then
    echo "Build error"
    exit 1
fi

zip -r go-transproxy-light-${VERSION}-windows-386.zip bin README.md LICENSE

# windows 64bit
rm -rf bin/*
rsrc -manifest transproxy-light.manifest -o rsrc.syso
GOOS=windows GOARCH=amd64 go build -v -o bin/transproxy-light.exe

if [ "$?" -ne 0 ]; then
    echo "Build error"
    exit 1
fi

zip -r go-transproxy-light-${VERSION}-windows-amd64.zip bin README.md LICENSE

