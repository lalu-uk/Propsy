#!/usr/bin/env bash

# Runs the Firestore emulator for the Firestore tests, with no gcloud: it's a
# plain Java program, so this downloads the emulator jar directly (the same one
# gcloud's cloud-firestore-emulator component and firebase-tools ship), checks
# its checksum, and runs it with `java`. Needs Java 21+ on PATH.
#
#   source ./firestore.sh start    # downloads on first use; exports FIRESTORE_EMULATOR_HOST
#   source ./firestore.sh stop
#
# To update the emulator, take the version, size and sha256 from
# https://github.com/firebase/firebase-tools/blob/master/src/emulator/downloadableEmulatorInfo.json
# ("firestore") and change the three values below.
EMULATOR_VERSION="1.22.0"
EMULATOR_SHA256="9b6498b7f62714d67f48f59b3818883cd682dbcd46b9f59511de81c97bb5166c"
EMULATOR_URL="https://storage.googleapis.com/firebase-preview-drop/emulator/cloud-firestore-emulator-v${EMULATOR_VERSION}.jar"

# "localhost", not "127.0.0.1": on at least one dev machine (macOS), the
# Firestore client's gRPC connection hangs indefinitely against the literal
# IP but connects instantly against the hostname. CI (Linux) works fine with
# either, so this is a local-only quirk, not a correctness issue — but it's
# the kind of thing worth never debugging twice.
HOST="localhost"
PORT="8219"
EMULATOR_HOST="${HOST}:${PORT}"
PID_FILE="${TMPDIR:-/tmp}/propsy-firestore-emulator.pid"
LOG_FILE="${TMPDIR:-/tmp}/propsy-firestore-emulator.log"
JAR_DIR="${XDG_CACHE_HOME:-$HOME/.cache}/propsy"
JAR="${JAR_DIR}/cloud-firestore-emulator-v${EMULATOR_VERSION}.jar"

if [[ "${BASH_SOURCE[0]}" == "$0" ]]; then
	printf 'Run this script with source so it can set FIRESTORE_EMULATOR_HOST in your shell.\n' >&2
	printf 'Example: source ./firestore.sh start\n' >&2
	exit 1
fi

# healthy does an actual HTTP round-trip rather than just checking the port
# accepts TCP connections: a wedged emulator process can keep its listening
# socket accepting at the kernel level long after the JVM itself has stopped
# responding, which a bare `nc -z` port check can't tell apart from healthy.
healthy() {
	curl -s -o /dev/null -m 2 "http://${EMULATOR_HOST}/"
}

sha256_of() {
	if command -v sha256sum >/dev/null 2>&1; then
		sha256sum "$1" | cut -d' ' -f1
	else
		shasum -a 256 "$1" | cut -d' ' -f1
	fi
}

# fetch_jar downloads the emulator once, and refuses a jar whose checksum
# doesn't match the pinned one.
fetch_jar() {
	if [[ -f "$JAR" && "$(sha256_of "$JAR")" == "$EMULATOR_SHA256" ]]; then
		return 0
	fi
	mkdir -p "$JAR_DIR" || return 1
	printf 'Downloading Firestore emulator %s (~130 MB)\n' "$EMULATOR_VERSION"
	local tmp="${JAR}.part"
	if ! curl -fsSL -o "$tmp" "$EMULATOR_URL"; then
		rm -f "$tmp"
		printf 'Could not download %s\n' "$EMULATOR_URL" >&2
		return 1
	fi
	if [[ "$(sha256_of "$tmp")" != "$EMULATOR_SHA256" ]]; then
		rm -f "$tmp"
		printf 'Checksum mismatch for the downloaded emulator jar; not using it\n' >&2
		return 1
	fi
	mv "$tmp" "$JAR"
}

start() {
	if healthy; then
		export FIRESTORE_EMULATOR_HOST="$EMULATOR_HOST"
		printf 'Firestore emulator already running on %s\n' "$FIRESTORE_EMULATOR_HOST"
		return 0
	fi

	if ! command -v java >/dev/null 2>&1; then
		printf 'java not found in PATH (the emulator needs Java 21+; `mise install` provides one)\n' >&2
		return 1
	fi
	fetch_jar || return 1

	java -Duser.language=en -jar "$JAR" --host="$HOST" --port="$PORT" >"$LOG_FILE" 2>&1 &
	echo $! >"$PID_FILE"

	local i
	for i in $(seq 1 60); do
		healthy && break
		sleep 0.5
	done
	if ! healthy; then
		printf 'Firestore emulator did not come up; see %s\n' "$LOG_FILE" >&2
		return 1
	fi
	export FIRESTORE_EMULATOR_HOST="$EMULATOR_HOST"
	printf 'Started Firestore emulator on %s\n' "$FIRESTORE_EMULATOR_HOST"
	printf 'Logs: %s\n' "$LOG_FILE"
}

stop() {
	if [[ -f "$PID_FILE" ]]; then
		kill "$(cat "$PID_FILE")" >/dev/null 2>&1 || true
		rm -f "$PID_FILE"
	fi
	unset FIRESTORE_EMULATOR_HOST
	printf 'Stopped Firestore emulator and unset FIRESTORE_EMULATOR_HOST\n'
}

case "${1:-}" in
	start)
		start
		;;
	stop)
		stop
		;;
	*)
		printf 'Usage: source ./firestore.sh {start|stop}\n' >&2
		return 1
		;;
	esac
