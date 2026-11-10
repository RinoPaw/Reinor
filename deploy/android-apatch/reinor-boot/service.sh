#!/system/bin/sh
# APatch late_start service, nonblocking. No system partition modifications.
MODDIR=${0%/*}
CONFIG="$MODDIR/agent.conf"
[ -r "$CONFIG" ] || exit 0
. "$CONFIG"
[ -n "$DEVICE_ID" ] && [ -n "$DATA_DIR" ] || exit 1

# Do not create a second monitor if the APatch Action button is pressed again.
# /dev is tmpfs, so locks cannot survive a reboot.
MONITOR_LOCK="/dev/reinor_monitor_${DEVICE_ID}.lock"
mkdir "$MONITOR_LOCK" 2>/dev/null || exit 0

# The monitor itself runs under APatch root; remote commands run with
# Android shell uid 2000 via /system/bin/su shell (but su remains available).
(
    trap 'rmdir "$MONITOR_LOCK" 2>/dev/null' EXIT HUP INT TERM
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
        # AID_INET (3003) is needed by unprivileged Android processes for
        # network access, including bionic DNS. APatch's su shell does not
        # automatically preserve ADB shell's supplementary groups.
        /system/bin/su -g 2000 -G 3003 shell -c "cd '$DATA_DIR' && HOME='$DATA_DIR' TMPDIR='$DATA_DIR' exec '$BIN' join -device '$DEVICE_ID'" >> "$LOG" 2>&1
        sleep 8
    done
) </dev/null >/dev/null 2>&1 &

exit 0
