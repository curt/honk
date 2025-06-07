#!/bin/sh
if [ "$SHELL_ONLY" = "true" ]; then
  exec /bin/sh
else
  exec ./honk
fi
