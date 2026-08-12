#!/bin/sh
set -eu

case "${1:-serve}" in
  serve | migrate)
    exec adenosine "$@"
    ;;
  web)
    shift
    exec bun /opt/adenosine/web/server/index.mjs "$@"
    ;;
  health)
    exec wget -qO- http://127.0.0.1:8080/health/ready
    ;;
  doctor)
    git --version
    test -d "${ADENOSINE_REPO_ROOT:?ADENOSINE_REPO_ROOT is required}"
    test -w "$ADENOSINE_REPO_ROOT"
    test -s "${ADENOSINE_SSH_HOST_KEY_PATH:?ADENOSINE_SSH_HOST_KEY_PATH is required}"
    psql "${DATABASE_URL:?DATABASE_URL is required}" -Atqc 'SELECT MAX(name) FROM public.schema_migrations'
    ;;
  *)
    exec "$@"
    ;;
esac
