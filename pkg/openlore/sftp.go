package openlore

import (
	"bytes"
	"io"
	"os"
	"sort"
	"time"

	"github.com/aakarim/go-openlore/pkg/vfs"
	"github.com/pkg/sftp"
)

// SFTPHandler implements the SFTP server interfaces using a vfs.FileSystem.
type SFTPHandler struct {
	fs vfs.FileSystem
}

// NewSFTPHandler creates a new SFTP handler backed by the given filesystem.
func NewSFTPHandler(fs vfs.FileSystem) *SFTPHandler {
	return &SFTPHandler{fs: fs}
}

// Fileread handles SFTP file read requests.
func (h *SFTPHandler) Fileread(r *sftp.Request) (io.ReaderAt, error) {
	if r.Method != "Get" {
		return nil, sftp.ErrSSHFxOpUnsupported
	}

	data, err := h.fs.ReadFile(r.Filepath)
	if err != nil {
		return nil, os.ErrNotExist
	}

	return bytes.NewReader(data), nil
}

// Filewrite rejects all write requests (read-only filesystem).
func (h *SFTPHandler) Filewrite(r *sftp.Request) (io.WriterAt, error) {
	return nil, sftp.ErrSSHFxPermissionDenied
}

// Filecmd rejects all file commands (read-only filesystem).
func (h *SFTPHandler) Filecmd(r *sftp.Request) error {
	return sftp.ErrSSHFxPermissionDenied
}

// Filelist handles SFTP directory listing and stat requests.
func (h *SFTPHandler) Filelist(r *sftp.Request) (sftp.ListerAt, error) {
	switch r.Method {
	case "List":
		entries, err := h.fs.ReadDir(r.Filepath)
		if err != nil {
			return nil, os.ErrNotExist
		}

		var infos []os.FileInfo
		for _, e := range entries {
			infos = append(infos, sftpFileInfo{e})
		}
		sort.Slice(infos, func(i, j int) bool {
			return infos[i].Name() < infos[j].Name()
		})
		return listAt(infos), nil

	case "Stat":
		fi, err := h.fs.Stat(r.Filepath)
		if err != nil {
			return nil, os.ErrNotExist
		}
		return listAt([]os.FileInfo{sftpFileInfo{*fi}}), nil

	default:
		return nil, sftp.ErrSSHFxOpUnsupported
	}
}

// sftpFileInfo wraps vfs.FileInfo to implement os.FileInfo.
type sftpFileInfo struct {
	info vfs.FileInfo
}

func (f sftpFileInfo) Name() string      { return f.info.FileName }
func (f sftpFileInfo) Size() int64       { return f.info.FileSize }
func (f sftpFileInfo) Mode() os.FileMode { return f.info.Mode() }
func (f sftpFileInfo) ModTime() time.Time {
	if f.info.FileModTime.IsZero() {
		return time.Now()
	}
	return f.info.FileModTime
}
func (f sftpFileInfo) IsDir() bool      { return f.info.Dir }
func (f sftpFileInfo) Sys() interface{} { return nil }

// listAt implements sftp.ListerAt.
type listAt []os.FileInfo

func (l listAt) ListAt(ls []os.FileInfo, offset int64) (int, error) {
	if offset >= int64(len(l)) {
		return 0, io.EOF
	}

	n := copy(ls, l[offset:])
	if n < len(ls) {
		return n, io.EOF
	}
	return n, nil
}
