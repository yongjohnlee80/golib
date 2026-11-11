//go:build windows

package local

import (
	"io/fs"
	"os"
	"unsafe"

	"golang.org/x/sys/windows"
)

// winInfo is an entry's FileInfo with what Windows keeps beside it: the volume and file index (its
// inode, in effect) and the change time, which moves on any write or attribute change, as ctime
// does on Unix. os.FileInfo exposes none of them, so they are read from the entry's handle.
type winInfo struct {
	os.FileInfo
	volume uint32
	index  uint64
	change int64 // 100 ns since 1601, as Windows keeps it
	ok     bool  // the handle's attributes were read
}

// fileBasicInfo is FILE_BASIC_INFO (GetFileInformationByHandleEx's FileBasicInfo class).
type fileBasicInfo struct {
	CreationTime, LastAccessTime, LastWriteTime, ChangeTime int64
	FileAttributes                                          uint32
	_                                                       uint32
}

// withHandle adds the handle's volume, index and change time to fi.
func withHandle(fi os.FileInfo, f *os.File) os.FileInfo {
	h := windows.Handle(f.Fd())
	var bi fileBasicInfo
	if err := windows.GetFileInformationByHandleEx(h, windows.FileBasicInfo, (*byte)(unsafe.Pointer(&bi)), uint32(unsafe.Sizeof(bi))); err != nil {
		return fi
	}
	var hi windows.ByHandleFileInformation
	if err := windows.GetFileInformationByHandle(h, &hi); err != nil {
		return fi
	}
	return winInfo{FileInfo: fi, volume: hi.VolumeSerialNumber, index: uint64(hi.FileIndexHigh)<<32 | uint64(hi.FileIndexLow),
		change: bi.ChangeTime, ok: true}
}

// lstat is the root's Lstat, with the entry's handle attributes for a file or a directory. A
// symlink is reported as it is, its own: the handle would be its target's.
func (f *FS) lstat(r *os.Root, name string) (os.FileInfo, error) {
	fi, err := r.Lstat(name)
	if err != nil || fi.Mode()&os.ModeSymlink != 0 {
		return fi, err
	}
	file, err := r.Open(name)
	if err != nil {
		return fi, nil // the attributes stand without the handle's
	}
	defer file.Close()
	return withHandle(fi, file), nil
}

// fileStat is file's Stat, with its handle's attributes.
func fileStat(file *os.File) (os.FileInfo, error) {
	fi, err := file.Stat()
	if err != nil {
		return nil, err
	}
	return withHandle(fi, file), nil
}

// entryInfo is a listed entry's FileInfo, as Stat gives it: a listing carries no file index or
// change time, so the entry is stat'ed through its handle, and the version ReadDir reports is the
// one Stat and a conditional write compare against.
func (f *FS) entryInfo(r *os.Root, name string, _ fs.DirEntry) (os.FileInfo, error) {
	return f.lstat(r, name)
}
