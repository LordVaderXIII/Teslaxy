package services

import "sync"

// mediaOpMu coordinates the scanner and retention so a prune pass cannot
// race a scan that would recreate rows for files being removed.
// Retention takes the write lock for the duration of unlink+DB cleanup.
// The scanner takes the read lock around indexing/inserts.
var mediaOpMu sync.RWMutex

func lockMediaWrite() { mediaOpMu.Lock() }
func unlockMediaWrite() { mediaOpMu.Unlock() }
func lockMediaRead()    { mediaOpMu.RLock() }
func unlockMediaRead()  { mediaOpMu.RUnlock() }
