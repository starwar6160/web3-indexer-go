package engine

import (
	"syscall"
)

// CheckStorageSpace returns the percentage of free space on the disk containing the given path.
func CheckStorageSpace(path string) (float64, error) {
	var stat syscall.Statfs_t
	err := syscall.Statfs(path, &stat)
	if err != nil {
		return 0, err
	}

	// Bavail is the number of free blocks available to unprivileged users.
	// Bsize is the optimal transfer block size.
	// #nosec G115 - Bsize is safe for uint64 conversion
	free := stat.Bavail * uint64(stat.Bsize)
	// #nosec G115
	total := stat.Blocks * uint64(stat.Bsize)

	if total == 0 {
		return 0, nil
	}

	freePercent := (float64(free) / float64(total)) * 100
	return freePercent, nil
}
