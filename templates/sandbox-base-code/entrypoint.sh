#
# Copyright 2025 The https://github.com/agent-sandbox/agent-sandbox Authors.
#
# Licensed under the Apache License, Version 2.0 (the "License");
# you may not use this file except in compliance with the License.
# You may obtain a copy of the License at
#
#     http://www.apache.org/licenses/LICENSE-2.0
#
# Unless required by applicable law or agreed to in writing, software
# distributed under the License is distributed on an "AS IS" BASIS,
# WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
# See the License for the specific language governing permissions and
# limitations under the License.
#

set -eu

ENVD_BIN="${ENVD_BIN:-/usr/bin/envd}"
ENVD_PORT="${ENVD_PORT:-49983}"
ENVD_LOG_FILE="${ENVD_LOG_FILE:--}"
ENVD_EXTRA_ARGS="${ENVD_EXTRA_ARGS:-}"

JUPYTER_HOST="${JUPYTER_HOST:-127.0.0.1}"
JUPYTER_PORT="${JUPYTER_PORT:-8888}"
JUPYTER_BASE_URL="http://$JUPYTER_HOST:$JUPYTER_PORT"
export JUPYTER_BASE_URL

CODE_INTERPRETER_HOST="${CODE_INTERPRETER_HOST:-0.0.0.0}"
CODE_INTERPRETER_PORT="${CODE_INTERPRETER_PORT:-49999}"
CODE_INTERPRETER_WORKDIR="${CODE_INTERPRETER_WORKDIR:-/workspace}"
CODE_INTERPRETER_APP_DIR="${CODE_INTERPRETER_APP_DIR:-/opt/code-interpreter}"

# set up the working directory
export WORKDIR="/workspace"
mkdir -p "${WORKDIR}"
chmod 777 "${WORKDIR}"

if [ ! -x "${ENVD_BIN}" ]; then
    echo "entrypoint: envd binary not found or not executable at ${ENVD_BIN}" >&2
    exit 127
fi

start_envd() {
    # shellcheck disable=SC2086
    if [ "${ENVD_LOG_FILE}" = "-" ]; then
        "${ENVD_BIN}" -port "${ENVD_PORT}" ${ENVD_EXTRA_ARGS} &
    else
        mkdir -p "$(dirname "${ENVD_LOG_FILE}")"
        "${ENVD_BIN}" -port "${ENVD_PORT}" ${ENVD_EXTRA_ARGS} \
            >>"${ENVD_LOG_FILE}" 2>&1 &
    fi
    ENVD_PID=$!
    echo "entrypoint: started envd (pid=${ENVD_PID}) on port ${ENVD_PORT}" >&2
}

start_jupyter() {
    mkdir -p "${CODE_INTERPRETER_WORKDIR}"
    jupyter server \
        --IdentityProvider.token="" \
        --ServerApp.ip="${JUPYTER_HOST}" \
        --ServerApp.port="${JUPYTER_PORT}" \
        --ServerApp.root_dir="${CODE_INTERPRETER_WORKDIR}" \
        --ServerApp.allow_root=True \
        --ServerApp.disable_check_xsrf=True \
        --ServerApp.open_browser=False &
    JUPYTER_PID=$!
    echo "entrypoint: started Jupyter Server (pid=${JUPYTER_PID}) on ${JUPYTER_BASE_URL}" >&2
}

start_envd
start_jupyter

if [ "$#" -eq 0 ]; then
    # No user command: run the code-interpreter API server as the foreground
    # process. tini is PID 1, so envd/Jupyter are reaped by tini on exit.
    exec python -m uvicorn \
        --host "${CODE_INTERPRETER_HOST}" \
        --port "${CODE_INTERPRETER_PORT}" \
        --log-level info \
        --timeout-keep-alive 640 \
        server:app \
        --app-dir "${CODE_INTERPRETER_APP_DIR}"
fi

# User command provided: forward termination signals so the user process can
# shut down cleanly; envd will be reaped by tini when the container stops.
USER_PID=""
forward_signal() {
    sig="$1"
    if [ -n "${USER_PID}" ]; then
        kill -s "${sig}" "${USER_PID}" 2>/dev/null || true
    fi
}

trap 'forward_signal TERM' TERM
trap 'forward_signal INT'  INT
trap 'forward_signal HUP'  HUP

"$@" &
USER_PID=$!
echo "entrypoint: exec user command (pid=${USER_PID}): $*" >&2

# Wait on the user command; propagate its exit status.
set +e
wait "${USER_PID}"
rc=$?
set -e
exit "${rc}"