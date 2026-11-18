#!/bin/sh

# Update desktop database for .desktop file changes
# This makes the application appear in application menus and registers its capabilities.
if command -v update-desktop-database >/dev/null 2>&1; then
  echo "Updating desktop database..."
  update-desktop-database -q /usr/share/applications
else
  echo "Warning: update-desktop-database command not found. Desktop file may not be immediately recognized." >&2
fi

# Update MIME database for custom URL schemes (x-scheme-handler)
# This ensures the system knows how to handle your custom protocols.
if command -v update-mime-database >/dev/null 2>&1; then
  echo "Updating MIME database..."
  update-mime-database -n /usr/share/mime
else
  echo "Warning: update-mime-database command not found. Custom URL schemes may not be immediately recognized." >&2
fi

# Let the built-in TFTP/FTP/HTTP servers use ports below 1024 (e.g. TFTP 69).
# The capability goes on the small pinion-bind helper, never on Pinion itself:
# a GUI process with a file capability is made non-dumpable, which locks
# /proc/<pid> and breaks file drag and drop through xdg-desktop-portal.
if command -v setcap >/dev/null 2>&1; then
  setcap -r /usr/local/bin/Pinion 2>/dev/null || true   # clear it if an older version set it
  setcap cap_net_bind_service=+ep /usr/local/bin/pinion-bind || echo "Warning: setcap failed; TFTP on port 69 will need root." >&2
fi

# Ubuntu 23.10+ blocks unprivileged user namespaces unless an AppArmor profile
# allows them, which breaks WebKitGTK's bubblewrap sandbox (blank window /
# crash on start). Install a profile that allows them for Pinion only.
# Skipped on systems without AppArmor 4 (no "userns" rule, no restriction).
PROFILE_SRC=/usr/share/pinion/apparmor/pinion
PROFILE_DST=/etc/apparmor.d/pinion
if [ -f "$PROFILE_SRC" ] && [ -d /etc/apparmor.d ] && [ -e /etc/apparmor.d/abi/4.0 ]; then
  install -m 0644 "$PROFILE_SRC" "$PROFILE_DST"
  if command -v apparmor_parser >/dev/null 2>&1 && [ "$(cat /sys/module/apparmor/parameters/enabled 2>/dev/null)" = "Y" ]; then
    apparmor_parser -r -T -W "$PROFILE_DST" || echo "Warning: failed to load AppArmor profile $PROFILE_DST" >&2
  fi
fi

exit 0
