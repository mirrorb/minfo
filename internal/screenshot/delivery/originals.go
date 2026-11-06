package delivery

import (
	"errors"
	"io"
	"os"
	"path/filepath"
	"sync"
	"time"
)

const originalPNGTTL = 30 * time.Minute

type originalPNG struct {
	path      string
	filename  string
	expiresAt time.Time
}

var originalPNGs = struct {
	sync.Mutex
	items map[string]originalPNG
}{items: make(map[string]originalPNG)}

// OriginalPNGPath 返回有损压缩前的 PNG 备份路径；子目录不会混入上传和 ZIP 文件列表。
func OriginalPNGPath(imagePath string) string {
	return filepath.Join(filepath.Dir(imagePath), ".originals", filepath.Base(imagePath))
}

// PreserveOriginalPNG 在覆盖 PNG 前保留其无损版本。
func PreserveOriginalPNG(imagePath string) error {
	path := OriginalPNGPath(imagePath)
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	file, err := os.Create(path)
	if err != nil {
		return err
	}
	if err := copyOriginalPNG(imagePath, file); err != nil {
		_ = os.Remove(path)
		return err
	}
	return nil
}

// SaveOriginalPNG 缓存无损 PNG，使任务临时目录清理后仍可通过令牌下载。
func SaveOriginalPNG(imagePath string) (string, error) {
	pruneOriginalPNGs(time.Now())
	file, err := os.CreateTemp("", "minfo-original-*.png")
	if err != nil {
		return "", err
	}
	if err := copyOriginalPNG(imagePath, file); err != nil {
		_ = os.Remove(file.Name())
		return "", err
	}
	token, err := buildPreparedDownloadToken()
	if err != nil {
		_ = os.Remove(file.Name())
		return "", err
	}
	originalPNGs.Lock()
	originalPNGs.items[token] = originalPNG{
		path: file.Name(), filename: filepath.Base(imagePath), expiresAt: time.Now().Add(originalPNGTTL),
	}
	originalPNGs.Unlock()
	time.AfterFunc(originalPNGTTL, func() { pruneOriginalPNGs(time.Now()) })
	return token, nil
}

// GetOriginalPNG 根据令牌读取无损原图缓存，不接受客户端提供的文件路径。
func GetOriginalPNG(token string) (string, string, error) {
	pruneOriginalPNGs(time.Now())
	originalPNGs.Lock()
	item, ok := originalPNGs.items[token]
	originalPNGs.Unlock()
	if !ok {
		return "", "", errors.New("original PNG expired or not found")
	}
	return item.path, item.filename, nil
}

// DeleteOriginalPNG 在下载入口消失时释放指定原图；重复删除视为成功。
func DeleteOriginalPNG(token string) error {
	originalPNGs.Lock()
	defer originalPNGs.Unlock()
	item, ok := originalPNGs.items[token]
	if !ok {
		return nil
	}
	if err := os.Remove(item.path); err != nil && !os.IsNotExist(err) {
		return err
	}
	delete(originalPNGs.items, token)
	return nil
}

func pruneOriginalPNGs(now time.Time) {
	originalPNGs.Lock()
	defer originalPNGs.Unlock()
	for token, item := range originalPNGs.items {
		if !item.expiresAt.After(now) {
			delete(originalPNGs.items, token)
			_ = os.Remove(item.path)
		}
	}
}

func copyOriginalPNG(sourcePath string, target *os.File) (err error) {
	defer func() {
		if closeErr := target.Close(); err == nil {
			err = closeErr
		}
	}()
	source, err := os.Open(sourcePath)
	if err != nil {
		return err
	}
	defer source.Close()
	_, err = io.Copy(target, source)
	return err
}
