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

// resolveUnderRootNoFollow lexically joins filePath under root, then walks every
// path component with openat(O_NOFOLLOW). Any symlink in the chain (parent
// directories or the leaf) is rejected. Returns the absolute path only when the leaf
// is a regular file still inside root.
func resolveUnderRootNoFollow(root, filePath string) (abs string, info os.FileInfo, err error) {
	if strings.TrimSpace(filePath) == "" {
		return "", nil, fmt.Errorf("empty_path")
	}
	root = filepath.Clean(root)
	cleaned := filepath.Clean(filePath)
	if !filepath.IsAbs(cleaned) {
		cleaned = filepath.Join(root, cleaned)
	}
	abs, err = filepath.Abs(cleaned)
	if err != nil {
		return "", nil, fmt.Errorf("abs_failed: %w", err)
	}
	if !pathInsideRoot(abs, root) {
		return "", nil, fmt.Errorf("outside_root")
	}

	rel, err := filepath.Rel(root, abs)
	if err != nil || strings.HasPrefix(rel, "..") {
		return "", nil, fmt.Errorf("outside_root")
	}
	if rel == "." {
		return "", nil, fmt.Errorf("is_directory")
	}

	dirfd, err := unix.Open(root, unix.O_RDONLY|unix.O_DIRECTORY|unix.O_CLOEXEC, 0)
	if err != nil {
		return "", nil, fmt.Errorf("open_root: %w", err)
	}
	defer func() { _ = unix.Close(dirfd) }()

	parts := strings.Split(rel, string(os.PathSeparator))
	currentFD := dirfd
	owned := false // whether currentFD is distinct from dirfd (deferred close)
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
			return "", nil, fmt.Errorf("outside_root")
		}
		isLast := i == len(parts)-1

		var st unix.Stat_t
		if err := unix.Fstatat(currentFD, part, &st, unix.AT_SYMLINK_NOFOLLOW); err != nil {
			if os.IsNotExist(err) {
				return "", nil, fmt.Errorf("missing")
			}
			return "", nil, fmt.Errorf("fstatat: %w", err)
		}
		if isSymlinkStat(st.Mode) {
			return "", nil, fmt.Errorf("symlink")
		}
		if !isLast {
			if !isDirStat(st.Mode) {
				return "", nil, fmt.Errorf("not_directory")
			}
			fd, err := unix.Openat(currentFD, part, unix.O_RDONLY|unix.O_DIRECTORY|unix.O_NOFOLLOW|unix.O_CLOEXEC, 0)
			if err != nil {
				if err == unix.ELOOP || err == syscall.ELOOP {
					return "", nil, fmt.Errorf("symlink")
				}
				return "", nil, fmt.Errorf("openat: %w", err)
			}
			closeCurrent()
			currentFD = fd
			owned = true
			continue
		}
		if isDirStat(st.Mode) {
			return "", nil, fmt.Errorf("is_directory")
		}
		if !isRegStat(st.Mode) {
			return "", nil, fmt.Errorf("not_regular")
		}
		fi := &statFileInfo{
			name:    part,
			size:    st.Size,
			mode:    os.FileMode(st.Mode & 0777),
			modTime: time.Unix(st.Mtim.Sec, st.Mtim.Nsec),
			sys:     st,
		}
		return abs, fi, nil
	}
	return "", nil, fmt.Errorf("empty_path")
}

// unlinkUnderRootNoFollow re-checks every component with AT_SYMLINK_NOFOLLOW and
// unlinks the leaf via unlinkat against the verified parent directory fd.
func unlinkUnderRootNoFollow(root, filePath string) (freed int64, err error) {
	root = filepath.Clean(root)
	cleaned := filepath.Clean(filePath)
	if !filepath.IsAbs(cleaned) {
		cleaned = filepath.Join(root, cleaned)
	}
	abs, err := filepath.Abs(cleaned)
	if err != nil {
		return 0, err
	}
	if !pathInsideRoot(abs, root) {
		return 0, fmt.Errorf("outside_root")
	}
	rel, err := filepath.Rel(root, abs)
	if err != nil || strings.HasPrefix(rel, "..") || rel == "." {
		return 0, fmt.Errorf("outside_root")
	}

	dirfd, err := unix.Open(root, unix.O_RDONLY|unix.O_DIRECTORY|unix.O_CLOEXEC, 0)
	if err != nil {
		return 0, err
	}
	defer func() { _ = unix.Close(dirfd) }()

	parts := strings.Split(rel, string(os.PathSeparator))
	currentFD := dirfd
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
			return 0, fmt.Errorf("outside_root")
		}
		isLast := i == len(parts)-1

		var st unix.Stat_t
		if err := unix.Fstatat(currentFD, part, &st, unix.AT_SYMLINK_NOFOLLOW); err != nil {
			if os.IsNotExist(err) {
				return 0, nil
			}
			return 0, err
		}
		if isSymlinkStat(st.Mode) {
			return 0, fmt.Errorf("symlink")
		}
		if !isLast {
			if !isDirStat(st.Mode) {
				return 0, fmt.Errorf("not_directory")
			}
			fd, err := unix.Openat(currentFD, part, unix.O_RDONLY|unix.O_DIRECTORY|unix.O_NOFOLLOW|unix.O_CLOEXEC, 0)
			if err != nil {
				if err == unix.ELOOP || err == syscall.ELOOP {
					return 0, fmt.Errorf("symlink")
				}
				return 0, err
			}
			closeCurrent()
			currentFD = fd
			owned = true
			continue
		}
		if !isRegStat(st.Mode) {
			return 0, fmt.Errorf("not_regular")
		}
		size := st.Size
		if err := unix.Unlinkat(currentFD, part, 0); err != nil {
			if os.IsNotExist(err) {
				return 0, nil
			}
			return 0, err
		}
		return size, nil
	}
	return 0, fmt.Errorf("empty_path")
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
