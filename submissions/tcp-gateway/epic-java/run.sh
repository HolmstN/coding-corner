#!/usr/bin/env bash
cd "$(dirname "$0")" || exit 1
exec mvn compile exec:java -Dexec.mainClass="socket.Socket" -Dexec.args="${GATEWAY_PORT:-7000}"
