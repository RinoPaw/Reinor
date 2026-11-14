#!/system/bin/sh
# The APatch Manager "Action" button starts the same background supervisor
# without requiring a reboot. The module's service.sh also starts on boot.
MODDIR=${0%/*}
/system/bin/sh "$MODDIR/service.sh"
