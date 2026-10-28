#!/bin/sh
# Run from a file manager or in a dedicated terminal window.
app_dir=$(CDPATH= cd -- "$(dirname -- "$0")" && pwd) || exit 1
cd "$app_dir" || exit 1
exec "$app_dir/steamcopy-linux-arm64"
