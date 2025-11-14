#!/usr/bin/env bash
set -euo pipefail

echo "Cleaning up straggler TAP devices and orphaned Cloud Hypervisor processes..."
echo ""

# Count resources before cleanup
tap_count=$(ip link show 2>/dev/null | grep -E "^[0-9]+: tap-" | wc -l)
ch_count=$(ps aux | grep cloud-hypervisor | grep -v grep | wc -l)

echo "Found:"
echo "  - $tap_count TAP devices"
echo "  - $ch_count Cloud Hypervisor processes"
echo ""

if [ "$tap_count" -eq 0 ] && [ "$ch_count" -eq 0 ]; then
	echo "No straggler resources found - system is clean!"
	exit 0
fi

# Kill orphaned Cloud Hypervisor processes
if [ "$ch_count" -gt 0 ]; then
	echo "Killing orphaned Cloud Hypervisor processes..."
	ps aux | grep cloud-hypervisor | grep -v grep | awk '{print $2}' | while read pid; do
		echo "  Killing PID $pid"
		sudo kill -9 "$pid" 2>/dev/null || true
	done
	# Wait a moment for processes to release resources
	sleep 0.5
	echo ""
fi

# Delete straggler TAP devices
if [ "$tap_count" -gt 0 ]; then
	echo "Removing straggler TAP devices..."
	ip link show 2>/dev/null | grep -E "^[0-9]+: tap-" | awk '{print $2}' | sed 's/:$//' | while read tap; do
		echo "  Removing TAP: $tap"
		sudo ip link delete "$tap" 2>/dev/null || true
	done
	echo ""
fi

# Count after cleanup
tap_after=$(ip link show 2>/dev/null | grep -E "^[0-9]+: tap-" | wc -l)
ch_after=$(ps aux | grep cloud-hypervisor | grep -v grep | wc -l)

echo "Cleanup complete!"
echo "Remaining:"
echo "  - $tap_after TAP devices"
echo "  - $ch_after Cloud Hypervisor processes"

if [ "$tap_after" -eq 0 ] && [ "$ch_after" -eq 0 ]; then
	echo ""
	echo "✓ All straggler resources cleaned up successfully!"
fi

