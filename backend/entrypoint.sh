#!/bin/sh
# Container entrypoint: repair only the dedicated state/download mount roots,
# then drop privileges before starting the application.
set -eu

APP_USER=raccoon
APP_UID=$(id -u "$APP_USER")
APP_GID=$(id -g "$APP_USER")
DATA_DIR=${RACCOON_DATA_DIR:-/data}
DOWNLOAD_DIR=${RACCOON_DOWNLOAD_DIR:-/downloads}

fail() {
  printf 'raccoon-entrypoint: ERROR: %s\n' "$*" >&2
  exit 1
}

case "$DATA_DIR" in
  /*) ;;
  *) fail "RACCOON_DATA_DIR must be an absolute path: '$DATA_DIR'" ;;
esac
case "$DOWNLOAD_DIR" in
  /*) ;;
  *) DOWNLOAD_DIR="$DATA_DIR/$DOWNLOAD_DIR" ;;
esac

# Refuse paths that could make a typo turn chown into an operation on a broad
# system directory. The application accepts relative download paths, resolved
# relative to DATA_DIR, so resolve that same way here.
check_managed_path() {
  path=$1
  case "$path" in
    ""|/|/proc|/sys|/dev|/etc|/usr|/var|/root|/home|/bin|/sbin|/lib|/lib64|/opt)
      fail "refusing to repair unsafe managed path '$path'"
      ;;
    /*) ;;
    *) fail "managed path must be absolute: '$path'" ;;
  esac
  case "/$path/" in
    */../*|*/./*) fail "managed path must not contain dot segments: '$path'" ;;
  esac
  [ ! -L "$path" ] || fail "managed mount root must not be a symlink: '$path'"
}

check_managed_path "$DATA_DIR"
check_managed_path "$DOWNLOAD_DIR"

if [ "$(id -u)" -eq 0 ]; then
  mkdir -p "$DATA_DIR" "$DOWNLOAD_DIR" || fail "cannot create data/download directories"

  # /data is dedicated application state (SQLite, credentials, logs and temp
  # metadata). Repair its ownership/modes while preserving group/other bits;
  # in particular, key files remain owner-only (e.g. 0600 stays 0600).
  if ! chown -R -h "$APP_UID:$APP_GID" "$DATA_DIR"; then
    fail "cannot repair ownership of $DATA_DIR for uid:gid $APP_UID:$APP_GID. The mount may be read-only, rootless/userns-remapped, or on NFS with root-squash; grant this uid:gid write access on the host and restart."
  fi
  if ! find "$DATA_DIR" -type d -exec chmod u+rwx {} +; then
    fail "cannot make application state directories owner-writable under $DATA_DIR"
  fi
  if ! find "$DATA_DIR" -type f -exec chmod u+rw {} +; then
    fail "cannot make application state files owner-readable/writable under $DATA_DIR"
  fi

  # Downloads may be large: repair only the configured root, never recursively
  # chown an existing download tree. Existing nested root-owned folders may need
  # a one-time host-side ownership fix if they are reused as download targets.
  if ! chown -h "$APP_UID:$APP_GID" "$DOWNLOAD_DIR" || ! chmod u+rwx "$DOWNLOAD_DIR"; then
    fail "cannot make download directory $DOWNLOAD_DIR writable by uid:gid $APP_UID:$APP_GID; check mount permissions/ACL or configure a dedicated writable download directory"
  fi

  # If no arguments or first argument starts with a flag, default to running raccoon
  if [ $# -eq 0 ]; then
    set -- /usr/local/bin/raccoon
  elif [ "${1#-}" != "$1" ]; then
    set -- /usr/local/bin/raccoon "$@"
  fi

  exec gosu "$APP_UID:$APP_GID" "$@"
fi

# Explicit --user deployments cannot be repaired by this process. Fail before
# SQLite emits a driver-level message that may be misleading.
if [ ! -w "$DATA_DIR" ]; then
  fail "$DATA_DIR is not writable by uid:gid $(id -u):$(id -g); fix the host mount ownership/ACL, or run the entrypoint as root so it can repair this dedicated state volume"
fi
if [ ! -w "$DOWNLOAD_DIR" ]; then
  fail "$DOWNLOAD_DIR is not writable by uid:gid $(id -u):$(id -g); fix the host mount ownership/ACL or configure a dedicated writable download directory"
fi

if [ $# -eq 0 ]; then
  set -- /usr/local/bin/raccoon
elif [ "${1#-}" != "$1" ]; then
  set -- /usr/local/bin/raccoon "$@"
fi

exec "$@"
