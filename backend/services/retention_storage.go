package services

import (
	"fmt"
	"io/fs"
	"os"
	"path/filepath"

	"golang.org/x/sys/unix"
)

// RetentionStorage counts logical regular-file bytes (including metadata) by
// Tesla directory, not allocated blocks or DB rows. Symlinks are not followed.
// Volume figures describe the whole footage filesystem, including other users.
type RetentionStorage struct {
	ByTypeBytes      map[string]int64 `json:"by_type_bytes"`
	VolumeTotalBytes uint64           `json:"volume_total_bytes"`
	VolumeUsedBytes  uint64           `json:"volume_used_bytes"`
	VolumeFreeBytes  uint64           `json:"volume_free_bytes"`
}

func (s *RetentionService) Storage() (*RetentionStorage, error) {
	cfg := s.Config()
	root := cfg.FootagePath
	storage := &RetentionStorage{ByTypeBytes: map[string]int64{"Sentry": 0, "Recent": 0, "Saved": 0}}
	for kind, dir := range map[string]string{"Sentry": "SentryClips", "Recent": "RecentClips", "Saved": "SavedClips"} {
		base := filepath.Join(root, dir)
		if _, err := os.Lstat(base); os.IsNotExist(err) {
			continue
		} else if err != nil {
			return nil, fmt.Errorf("storage %s: %w", kind, err)
		}
		err := filepath.WalkDir(base, func(path string, entry fs.DirEntry, err error) error {
			if err != nil {
				return err
			}
			if !entry.Type().IsRegular() {
				return nil
			}
			info, err := entry.Info()
			// Scanner/retention can remove a file during a read-only storage sample.
			if os.IsNotExist(err) {
				return nil
			}
			if err != nil {
				return err
			}
			storage.ByTypeBytes[kind] += info.Size()
			return nil
		})
		if err != nil {
			return nil, fmt.Errorf("storage %s: %w", kind, err)
		}
	}
	statfs := s.statfsFn
	if statfs == nil {
		statfs = unix.Statfs
	}
	var stat unix.Statfs_t
	if err := statfs(root, &stat); err != nil {
		return nil, fmt.Errorf("storage volume: %w", err)
	}
	if stat.Bsize <= 0 || stat.Bfree > stat.Blocks {
		return nil, fmt.Errorf("invalid storage volume statistics")
	}
	storage.VolumeTotalBytes = stat.Blocks * uint64(stat.Bsize)
	storage.VolumeFreeBytes = stat.Bfree * uint64(stat.Bsize)
	storage.VolumeUsedBytes = storage.VolumeTotalBytes - storage.VolumeFreeBytes
	return storage, nil
}
