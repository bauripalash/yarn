#!/bin/bash
set -euo pipefail

cd /opt/app
if [ ! -e go.mod ]; then
	printf "Error: This directory does not contain a go module;\n"
	printf "vagrant-spk's golang stack does not support older GOPATH\n"
	printf "based projects. Try running:\n" >&2
	printf "\n" >&2
	printf "    vagrant-spk vm ssh\n" >&2
	printf "    cd /opt/app\n" >&2
	printf "    go mod init example.com/mypkg\n" >&2
	exit 1
fi

PATH="$HOME/go/bin:$PATH"

make deps
make preflight server
mv yarnd app

exit 0
