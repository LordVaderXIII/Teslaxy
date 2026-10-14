package services

import (
	"os"
	"time"

	"golang.org/x/sys/unix"
)

type statFileInfo struct {
	name    string
	size    int64
	mode    os.FileMode
	modTime time.Time
	sys     unix.Stat_t
}

func (s *statFileInfo) Name() string       { return s.name }
func (s *statFileInfo) Size() int64        { return s.size }
func (s *statFileInfo) Mode() os.FileMode  { return s.mode }
func (s *statFileInfo) ModTime() time.Time { return s.modTime }
func (s *statFileInfo) IsDir() bool        { return s.mode.IsDir() }
func (s *statFileInfo) Sys() interface{}   { return &s.sys }
