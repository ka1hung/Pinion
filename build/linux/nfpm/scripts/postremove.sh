#!/bin/sh

# Unload and remove the AppArmor profile installed by postinstall.sh.
# On upgrade the new package's postinstall reinstalls it, so leave it alone.
# deb passes "remove"/"purge"/"upgrade"; rpm passes the remaining count (0 = erase).
case "$1" in
  upgrade|1) exit 0 ;;
esac

PROFILE=/etc/apparmor.d/pinion
if [ -f "$PROFILE" ]; then
  if command -v apparmor_parser >/dev/null 2>&1 && [ "$(cat /sys/module/apparmor/parameters/enabled 2>/dev/null)" = "Y" ]; then
    apparmor_parser -R "$PROFILE" 2>/dev/null || true
  fi
  rm -f "$PROFILE"
fi

exit 0
