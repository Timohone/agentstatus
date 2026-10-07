#!/bin/bash
# Alltagsaufruf: baut dist/AgentStatus.app (ad-hoc signiert).
exec "$(dirname "$0")/../scripts/build-app.sh" --out dist "$@"
