// Package screenshot 提供截图文件压缩能力。

package screenshot

import (
	"archive/zip"
	"bytes"
	"io"
	"os"
	"path/filepath"
)

// ZipFiles 把给定文件列表打包成一个内存中的 ZIP 压缩包。
func ZipFiles(paths []string) ([]byte, error) {
	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)

	for _, path := range paths {
		file, err := os.Open(path)
		if err != nil {
			_ = zw.Close()
			return nil, err
		}
		info, err := file.Stat()
		if err != nil {
			file.Close()
			_ = zw.Close()
			return nil, err
		}
		header, err := zip.FileInfoHeader(info)
		if err != nil {
			file.Close()
			_ = zw.Close()
			return nil, err
		}
		header.Name = filepath.Base(path)
		header.Method = zip.Deflate
		writer, err := zw.CreateHeader(header)
		if err != nil {
			file.Close()
			_ = zw.Close()
			return nil, err
		}
		if _, err := io.Copy(writer, file); err != nil {
			file.Close()
			_ = zw.Close()
			return nil, err
		}
		file.Close()
	}

	if err := zw.Close(); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}
