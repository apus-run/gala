package fileservertest

import (
	"testing"

	"github.com/apus-run/gala/components/fileserver"
)

var allCaps = Capabilities{ContentType: true, Metadata: true, IfNotExists: true, ModTime: true}

// TestMemStoreStorageSuite 用 Fake 自检 Storage 契约套件。
func TestMemStoreStorageSuite(t *testing.T) {
	RunStorageSuite(t, func(t *testing.T) fileserver.Storage {
		return NewMemStore()
	}, allCaps)
}
