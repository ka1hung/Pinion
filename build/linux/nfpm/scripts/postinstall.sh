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

# Allow the built-in TFTP/FTP/HTTP servers to bind ports below 1024 (e.g. TFTP 69)
# without running Pinion as root.
if command -v setcap >/dev/null 2>&1; then
  setcap cap_net_bind_service=+ep /usr/local/bin/Pinion || echo "Warning: setcap failed; TFTP on port 69 will need root." >&2
fi

exit 0
