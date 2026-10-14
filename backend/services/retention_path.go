package services

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	"golang.org/x/sys/unix"
)

// footageRoot is a verified, pinned directory fd for FOOTAGE_PATH.
// All resolve/unlink operations use openat/unlinkat against this fd so a
// mid-prune swap of the path for a symlink cannot redirect deletions.
type footageRoot struct {
	path string
	fd   int
}

// openFootageRoot opens path with O_DIRECTORY|O_NOFOLLOW and keeps the fd.
// A symlinked root is rejected (ELOOP). Callers must Close().
func openFootageRoot(path string) (*footageRoot, error) {
	if strings.TrimSpace(path) == "" {
		return nil, fmt.Errorf("empty_root")
	}
	cleaned := filepath.Clean(path)
	abs, err := filepath.Abs(cleaned)
	if err != nil {
		return nil, fmt.Errorf("abs_failed: %w", err)
	}
	fd, err := unix.Open(abs, unix.O_RDONLY|unix.O_DIRECTORY|unix.O_NOFOLLOW|unix.O_CLOEXEC, 0)
	if err != nil {
		if err == unix.ELOOP || err == syscall.ELOOP {
			return nil, fmt.Errorf("symlink_root")
		}
		return nil, fmt.Errorf("open_root: %w", err)
	}
	var st unix.Stat_t
	if err := unix.Fstat(fd, &st); err != nil {
		_ = unix.Close(fd)
		return nil, fmt.Errorf("fstat_root: %w", err)
	}
	if !isDirStat(st.Mode) {
		_ = unix.Close(fd)
		return nil, fmt.Errorf("root_not_directory")
	}
	return &footageRoot{path: abs, fd: fd}, nil
}

func (r *footageRoot) Close() error {
	if r == nil || r.fd < 0 {
		return nil
	}
	err := unix.Close(r.fd)
	r.fd = -1
	return err
}

func (r *footageRoot) Path() string {
	if r == nil {
		return ""
	}
	return r.path
}

func (r *footageRoot) relAndAbs(filePath string) (rel, abs string, err error) {
	if r == nil || r.fd < 0 {
		return "", "", fmt.Errorf("closed_root")
	}
	if strings.TrimSpace(filePath) == "" {
		return "", "", fmt.Errorf("empty_path")
	}
	cleaned := filepath.Clean(filePath)
	if !filepath.IsAbs(cleaned) {
		cleaned = filepath.Join(r.path, cleaned)
	}
	abs, err = filepath.Abs(cleaned)
	if err != nil {
		return "", "", fmt.Errorf("abs_failed: %w", err)
	}
	if !pathInsideRoot(abs, r.path) {
		return "", "", fmt.Errorf("outside_root")
	}
	rel, err = filepath.Rel(r.path, abs)
	if err != nil || strings.HasPrefix(rel, "..") {
		return "", "", fmt.Errorf("outside_root")
	}
	if rel == "." {
		return "", "", fmt.Errorf("is_directory")
	}
	return rel, abs, nil
}

// resolve walks from the pinned root fd with O_NOFOLLOW / AT_SYMLINK_NOFOLLOW.
// It never re-opens r.path by name.
func (r *footageRoot) resolve(filePath string) (abs string, info os.FileInfo, err error) {
	rel, abs, err := r.relAndAbs(filePath)
	if err != nil {
		return "", nil, err
	}
	fi, _, err := r.walk(rel, false)
	if err != nil {
		return "", nil, err
	}
	return abs, fi, nil
}

// unlink removes the leaf via unlinkat on the pinned root fd chain.
// freed is the file size only when this was the last link (Nlink == 1).
func (r *footageRoot) unlink(filePath string) (freed int64, removed bool, err error) {
	rel, _, err := r.relAndAbs(filePath)
	if err != nil {
		return 0, false, err
	}
	fi, freed, err := r.walk(rel, true)
	if err != nil {
		if strings.Contains(err.Error(), "missing") {
			return 0, true, nil
		}
		return 0, false, err
	}
	_ = fi
	return freed, true, nil
}

func (r *footageRoot) walk(rel string, doUnlink bool) (info os.FileInfo, freed int64, err error) {
	parts := strings.Split(rel, string(os.PathSeparator))
	currentFD := r.fd
	owned := false
	closeCurrent := func() {
		if owned {
			_ = unix.Close(currentFD)
			owned = false
		}
	}
	defer closeCurrent()

	for i, part := range parts {
		if part == "" || part == "." {
			continue
		}
		if part == ".." {
			return nil, 0, fmt.Errorf("outside_root")
		}
		isLast := i == len(parts)-1

		var st unix.Stat_t
		if err := unix.Fstatat(currentFD, part, &st, unix.AT_SYMLINK_NOFOLLOW); err != nil {
			if os.IsNotExist(err) {
				return nil, 0, fmt.Errorf("missing")
			}
			return nil, 0, fmt.Errorf("fstatat: %w", err)
		}
		if isSymlinkStat(st.Mode) {
			return nil, 0, fmt.Errorf("symlink")
		}
		if !isLast {
			if !isDirStat(st.Mode) {
				return nil, 0, fmt.Errorf("not_directory")
			}
			fd, err := unix.Openat(currentFD, part, unix.O_RDONLY|unix.O_DIRECTORY|unix.O_NOFOLLOW|unix.O_CLOEXEC, 0)
			if err != nil {
				if err == unix.ELOOP || err == syscall.ELOOP {
					return nil, 0, fmt.Errorf("symlink")
				}
				return nil, 0, fmt.Errorf("openat: %w", err)
			}
			closeCurrent()
			currentFD = fd
			owned = true
			continue
		}
		if isDirStat(st.Mode) {
			return nil, 0, fmt.Errorf("is_directory")
		}
		if !isRegStat(st.Mode) {
			return nil, 0, fmt.Errorf("not_regular")
		}
		fi := &statFileInfo{
			name:    part,
			size:    st.Size,
			mode:    os.FileMode(st.Mode & 0777),
			modTime: time.Unix(st.Mtim.Sec, st.Mtim.Nsec),
			sys:     st,
		}
		if !doUnlink {
			return fi, 0, nil
		}
		if st.Nlink <= 1 {
			freed = st.Size
		}
		if err := unix.Unlinkat(currentFD, part, 0); err != nil {
			if os.IsNotExist(err) {
				return fi, 0, fmt.Errorf("missing")
			}
			return fi, 0, err
		}
		return fi, freed, nil
	}
	return nil, 0, fmt.Errorf("empty_path")
}

// resolveUnderRootNoFollow opens an ephemeral root fd for one-shot checks.
func resolveUnderRootNoFollow(root, filePath string) (string, os.FileInfo, error) {
	fr, err := openFootageRoot(root)
	if err != nil {
		return "", nil, err
	}
	defer fr.Close()
	return fr.resolve(filePath)
}

// unlinkUnderRootNoFollow is a one-shot helper; production prune pins footageRoot.
func unlinkUnderRootNoFollow(root, filePath string) (int64, error) {
	fr, err := openFootageRoot(root)
	if err != nil {
		return 0, err
	}
	defer fr.Close()
	freed, _, err := fr.unlink(filePath)
	return freed, err
}

func isSymlinkStat(mode uint32) bool {
	return mode&unix.S_IFMT == unix.S_IFLNK
}

func isDirStat(mode uint32) bool {
	return mode&unix.S_IFMT == unix.S_IFDIR
}

func isRegStat(mode uint32) bool {
	return mode&unix.S_IFMT == unix.S_IFREG
}

func fileIdentity(info os.FileInfo) (dev, ino uint64, ok bool) {
	if info == nil {
		return 0, 0, false
	}
	switch st := info.Sys().(type) {
	case *syscall.Stat_t:
		return uint64(st.Dev), uint64(st.Ino), true
	case syscall.Stat_t:
		return uint64(st.Dev), uint64(st.Ino), true
	case *unix.Stat_t:
		return uint64(st.Dev), uint64(st.Ino), true
	case unix.Stat_t:
		return uint64(st.Dev), uint64(st.Ino), true
	}
	return 0, 0, false
}

func fileNlink(info os.FileInfo) uint64 {
	if info == nil {
		return 0
	}
	switch st := info.Sys().(type) {
	case *syscall.Stat_t:
		return uint64(st.Nlink)
	case syscall.Stat_t:
		return uint64(st.Nlink)
	case *unix.Stat_t:
		return uint64(st.Nlink)
	case unix.Stat_t:
		return uint64(st.Nlink)
	}
	return 1
}
