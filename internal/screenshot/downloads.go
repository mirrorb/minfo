// Package screenshot 管理已准备下载文件的缓存与清理。

package screenshot

import (
	"crypto/rand"
	"encoding/hex"
	"errors"
	"os"
	"sync"
	"time"
)

var errPreparedDownloadNotFound = errors.New("prepared download not found")

const preparedDownloadTTL = 15 * time.Minute

type preparedDownload struct {
	path      string
	expiresAt time.Time
}

var preparedDownloads = struct {
	mu    sync.Mutex
	items map[string]preparedDownload
}{
	items: map[string]preparedDownload{},
}

// SavePreparedDownload 将生成好的 ZIP 数据写入临时文件，并返回下载令牌。
func SavePreparedDownload(data []byte) (string, error) {
	prunePreparedDownloads(time.Now())

	file, err := os.CreateTemp("", "minfo-download-*.zip")
	if err != nil {
		return "", err
	}

	if _, err := file.Write(data); err != nil {
		file.Close()
		_ = os.Remove(file.Name())
		return "", err
	}
	if err := file.Close(); err != nil {
		_ = os.Remove(file.Name())
		return "", err
	}

	token, err := buildPreparedDownloadToken()
	if err != nil {
		_ = os.Remove(file.Name())
		return "", err
	}

	preparedDownloads.mu.Lock()
	preparedDownloads.items[token] = preparedDownload{
		path:      file.Name(),
		expiresAt: time.Now().Add(preparedDownloadTTL),
	}
	preparedDownloads.mu.Unlock()

	return token, nil
}

// GetPreparedDownload 根据令牌返回仍在有效期内的临时下载文件路径。
func GetPreparedDownload(token string) (string, error) {
	now := time.Now()
	prunePreparedDownloads(now)

	preparedDownloads.mu.Lock()
	item, ok := preparedDownloads.items[token]
	preparedDownloads.mu.Unlock()

	if !ok || !item.expiresAt.After(now) {
		return "", errPreparedDownloadNotFound
	}

	return item.path, nil
}

// prunePreparedDownloads 删除过期缓存及其对应的临时文件，避免缓存持续增长。
func prunePreparedDownloads(now time.Time) {
	preparedDownloads.mu.Lock()
	defer preparedDownloads.mu.Unlock()

	for token, item := range preparedDownloads.items {
		if item.expiresAt.After(now) {
			continue
		}
		delete(preparedDownloads.items, token)
		_ = os.Remove(item.path)
	}
}

// buildPreparedDownloadToken 生成一个随机的下载令牌。
func buildPreparedDownloadToken() (string, error) {
	buf := make([]byte, 16)
	if _, err := rand.Read(buf); err != nil {
		return "", err
	}
	return hex.EncodeToString(buf), nil
}
