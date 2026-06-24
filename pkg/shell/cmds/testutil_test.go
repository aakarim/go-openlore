package cmds_test

import (
	"bytes"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/aakarim/go-openlore/pkg/shell"
	"github.com/aakarim/go-openlore/pkg/shell/cmds"
)

// mapFS is an in-memory filesystem for testing.
type mapFS struct {
	Files map[string]*cmds.FileInfo
	Dirs  map[string][]string
}

func newMapFS() *mapFS {
	return &mapFS{
		Files: make(map[string]*cmds.FileInfo),
		Dirs:  make(map[string][]string),
	}
}

func (m *mapFS) AddFile(path string, content string) {
	fi := &cmds.FileInfo{
		FileName:    baseName(path),
		FilePath:    path,
		Content:     []byte(content),
		Dir:         false,
		FileModTime: time.Date(2024, 1, 15, 10, 30, 0, 0, time.UTC),
		FileSize:    int64(len(content)),
	}
	m.Files[path] = fi
	dir := dirName(path)
	for dir != "" && dir != "/" {
		if _, ok := m.Files[dir]; !ok {
			m.Files[dir] = &cmds.FileInfo{
				FileName: baseName(dir), FilePath: dir, Dir: true,
				FileModTime: time.Date(2024, 1, 15, 10, 0, 0, 0, time.UTC),
			}
		}
		parent := dirName(dir)
		m.addChild(parent, baseName(dir))
		dir = parent
	}
	if _, ok := m.Files["/"]; !ok {
		m.Files["/"] = &cmds.FileInfo{FileName: "/", FilePath: "/", Dir: true, FileModTime: time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC)}
	}
	parent := dirName(path)
	m.addChild(parent, baseName(path))
}

func (m *mapFS) addChild(dir, child string) {
	for _, c := range m.Dirs[dir] {
		if c == child {
			return
		}
	}
	m.Dirs[dir] = append(m.Dirs[dir], child)
}

func (m *mapFS) AddDir(path string) {
	m.Files[path] = &cmds.FileInfo{
		FileName: baseName(path), FilePath: path, Dir: true,
		FileModTime: time.Date(2024, 1, 15, 10, 0, 0, 0, time.UTC),
	}
	if path != "/" {
		parent := dirName(path)
		m.addChild(parent, baseName(path))
	}
}

func (m *mapFS) Stat(path string) (*cmds.FileInfo, error) {
	path = cmds.CleanPath(path)
	fi, ok := m.Files[path]
	if !ok {
		return nil, fmt.Errorf("not found: %s", path)
	}
	return fi, nil
}

func (m *mapFS) ReadDir(path string) ([]cmds.FileInfo, error) {
	path = cmds.CleanPath(path)
	fi, ok := m.Files[path]
	if !ok {
		return nil, fmt.Errorf("not found: %s", path)
	}
	if !fi.Dir {
		return nil, fmt.Errorf("not a directory: %s", path)
	}
	children := m.Dirs[path]
	var result []cmds.FileInfo
	for _, name := range children {
		childPath := path + "/" + name
		if path == "/" {
			childPath = "/" + name
		}
		if cfi, ok := m.Files[childPath]; ok {
			result = append(result, *cfi)
		}
	}
	return result, nil
}

func (m *mapFS) ReadFile(path string) ([]byte, error) {
	path = cmds.CleanPath(path)
	fi, ok := m.Files[path]
	if !ok {
		return nil, fmt.Errorf("not found: %s", path)
	}
	if fi.Dir {
		return nil, fmt.Errorf("is a directory: %s", path)
	}
	return fi.Content, nil
}

func baseName(path string) string {
	if path == "/" {
		return "/"
	}
	parts := strings.Split(strings.TrimRight(path, "/"), "/")
	return parts[len(parts)-1]
}

func dirName(path string) string {
	if path == "/" {
		return ""
	}
	idx := strings.LastIndex(path, "/")
	if idx <= 0 {
		return "/"
	}
	return path[:idx]
}

func testFS() *mapFS {
	fs := newMapFS()
	fs.AddDir("/")
	fs.AddDir("/docs")
	fs.AddFile("/docs/readme.md", "# Hello World\nThis is a test file.\nLine 3\nLine 4\nLine 5\n")
	fs.AddFile("/docs/notes.txt", "banana\napple\ncherry\napple\ndate\nbanana\n")
	fs.AddFile("/docs/data.csv", "name,age,city\nalice,30,new york\nbob,25,boston\ncharlie,35,chicago\n")
	fs.AddFile("/docs/data.json", `{"name":"alice","age":30,"items":[1,2,3],"active":true}`)
	fs.AddFile("/docs/numbers.txt", "10\n2\n30\n1\n20\n3\n")
	fs.AddFile("/docs/script.sh", "echo hello\necho world\n")
	fs.AddFile("/docs/tabs.txt", "col1\tcol2\tcol3\nval1\tval2\tval3\n")
	fs.AddFile("/docs/sorted1.txt", "apple\nbanana\ncherry\n")
	fs.AddFile("/docs/sorted2.txt", "banana\ncherry\ndate\n")
	fs.AddDir("/docs/sub")
	fs.AddFile("/docs/sub/file.txt", "sub file content\n")
	fs.AddFile("/docs/users.json", `[{"name":"alice","age":30,"active":true},{"name":"bob","age":25,"active":false},{"name":"charlie","age":35,"active":true}]`)
	fs.AddFile("/docs/checksum.txt", "test content for hashing\n")
	return fs
}

func execCmd(t *testing.T, fs *mapFS, cmd string) (string, string, int) {
	t.Helper()
	sh := shell.NewShell(fs)
	var out, errOut bytes.Buffer
	code := sh.ExecPipeline(cmd, &out, &errOut, nil)
	return out.String(), errOut.String(), code
}

func assertOutput(t *testing.T, fs *mapFS, cmd, expected string) {
	t.Helper()
	out, errOut, code := execCmd(t, fs, cmd)
	if code != 0 {
		t.Errorf("%s: exit code %d, stderr: %s", cmd, code, errOut)
	}
	if strings.TrimRight(out, "\n") != strings.TrimRight(expected, "\n") {
		t.Errorf("%s:\n  got:  %q\n  want: %q", cmd, strings.TrimRight(out, "\n"), strings.TrimRight(expected, "\n"))
	}
}

func assertExitCode(t *testing.T, fs *mapFS, cmd string, expectedCode int) {
	t.Helper()
	_, _, code := execCmd(t, fs, cmd)
	if code != expectedCode {
		t.Errorf("%s: exit code %d, want %d", cmd, code, expectedCode)
	}
}
