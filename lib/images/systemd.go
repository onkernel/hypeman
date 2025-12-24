package images

import "strings"

// IsSystemdImage checks if the image's CMD indicates it wants systemd as init.
// Detection is based on the effective command (entrypoint + cmd), not whether
// systemd is installed in the image.
//
// Returns true if the image's command is:
//   - /sbin/init
//   - /lib/systemd/systemd
//   - /usr/lib/systemd/systemd
//   - Any path ending in /init
func IsSystemdImage(entrypoint, cmd []string) bool {
	// Combine to get the actual command that will run
	effective := append(entrypoint, cmd...)
	if len(effective) == 0 {
		return false
	}

	first := effective[0]

	// Match specific systemd/init paths
	systemdPaths := []string{
		"/sbin/init",
		"/lib/systemd/systemd",
		"/usr/lib/systemd/systemd",
	}
	for _, p := range systemdPaths {
		if first == p {
			return true
		}
	}

	// Match any absolute path ending in /init (e.g., /usr/sbin/init)
	// Only match absolute paths to avoid false positives like "./init"
	if strings.HasPrefix(first, "/") && strings.HasSuffix(first, "/init") {
		return true
	}

	return false
}

