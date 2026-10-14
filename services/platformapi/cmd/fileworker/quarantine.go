package main

import (
	"errors"
	"os"

	filedomain "github.com/Techshrr/GoJet/internal/files"
)

// openClaimedQuarantine is called only after ClaimNextScan has committed the
// current scan lease. Admin quarantine revokes distribution in MySQL and queues
// that scan; the worker reconciles bytes still held in the published directory.
// Moving bytes grants no publication authority and never substitutes for a scan.
func openClaimedQuarantine(storage *filedomain.NativeStorage, key string) (*os.File, error) {
	file, err := storage.OpenQuarantine(key)
	if !errors.Is(err, os.ErrNotExist) {
		return file, err
	}
	if err := storage.ReturnToQuarantine(key); err != nil {
		return nil, err
	}
	return storage.OpenQuarantine(key)
}
