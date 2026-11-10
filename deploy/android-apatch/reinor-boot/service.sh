#!/system/bin/sh
# APatch late_start service, nonblocking. No system partition modifications.
MODDIR=${0%/*}
CONFIG="$MODDIR/agent.conf"
[ -r "$CONFIG" ] || exit 0
. "$CONFIG"
[ -n "$DEVICE_ID" ] && [ -n "$DATA_DIR" ] || exit 1

# The monitor itself runs under APatch root; every remote command runs with
# Android shell uid 2000 via /system/bin/su shell, not as root by default.
(
    while [ "$(/system/bin/getprop sys.boot_completed)" != "1" ]; do
        sleep 3
    done

    while [ ! -e "$DATA_DIR/.stop" ] && [ ! -e "$MODDIR/disable" ]; do
        BIN="$DATA_DIR/reinor-agent"
        LOG="$DATA_DIR/agent.log"
        if [ ! -x "$BIN" ]; then
            sleep 15
            continue
        fi

        # Avoid a second session if the user launched the same Agent manually.
        if /system/bin/pidof reinor-agent >/dev/null 2>&1; then
            sleep 10
            continue
        fi

        if [ -f "$LOG" ] && [ "$(wc -c < "$LOG")" -gt 1048576 ]; then
            mv "$LOG" "$LOG.previous"
        fi
        /system/bin/su shell -c "cd '$DATA_DIR' && HOME='$DATA_DIR' TMPDIR='$DATA_DIR' exec '$BIN' join -device '$DEVICE_ID'" >> "$LOG" 2>&1
        sleep 8
    done
) </dev/null >/dev/null 2>&1 &

exit 0
