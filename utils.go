package main

import (
	"bytes"
	"compress/flate"
	"compress/gzip"
	"fmt"
	"net/http"
	"path/filepath"
	"strconv"
	"strings"
)

// 解析版本号
func parseVersion(version string) ([]int, error) {
	parts := strings.Split(version, ".")
	if len(parts) != 3 {
		return nil, fmt.Errorf("版本格式不正确: %s", version)
	}

	var nums []int
	for _, part := range parts {
		num, err := strconv.Atoi(part)
		if err != nil {
			return nil, fmt.Errorf("解析版本号失败: %v", err)
		}
		nums = append(nums, num)
	}

	return nums, nil
}

// 验证固件文件是否是有效的文件类型
func isValidFirmwareType(filename string) bool {
	validExtensions := []string{".img"}
	ext := strings.ToLower(filepath.Ext(filename))
	for _, validExt := range validExtensions {
		if ext == validExt {
			return true
		}
	}
	return false
}

// 验证音频资源文件是否是有效的文件类型
func isValidFileType(filename string) bool {
	validExtensions := []string{".mp3", ".wav"}
	ext := strings.ToLower(filepath.Ext(filename))
	for _, validExt := range validExtensions {
		if ext == validExt {
			return true
		}
	}
	return false
}

// 判断文件是否应该在浏览器中显示
func shouldInline(contentType string) bool {
	// 你可以根据需要添加更多的MIME类型
	inlineTypes := []string{
		"text/plain",
		"text/html",
		"text/css",
		"application/javascript",
		"image/jpeg",
		"image/png",
		"image/gif",
		"application/pdf",
		"audio/mpeg",
		"audio/wav", // 添加 audio/wav 支持
		"video/mp4",
	}

	for _, t := range inlineTypes {
		if contentType == t {
			return true
		}
	}
	return false
}

// 定义 Gzip、deflate 响应写入器
func CompressionMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// 忽略上传、下载、预览文件接口
		if r.URL.Path == "/upload" || r.URL.Path == "/download" || r.URL.Path == "/previewFile" {
			next.ServeHTTP(w, r)
			return
		}

		// 检查是否为 WebSocket 握手请求
		if r.Header.Get("Upgrade") == "websocket" {
			next.ServeHTTP(w, r)
			return
		}

		// 检查请求头中支持的编码
		acceptEncoding := r.Header.Get("Accept-Encoding")
		if strings.Contains(acceptEncoding, "gzip") {
			// 使用 Gzip 压缩
			w.Header().Set("Content-Encoding", "gzip")
			gz := gzip.NewWriter(w)
			defer gz.Close()
			gw := &gzipResponseWriter{
				Writer:         gz,
				ResponseWriter: w,
				gzipWriter:     gz,
			}
			next.ServeHTTP(gw, r)
		} else if strings.Contains(acceptEncoding, "deflate") {
			// 使用 Deflate 压缩
			var buf bytes.Buffer
			writer, err := flate.NewWriter(&buf, flate.BestCompression)
			if err != nil {
				http.Error(w, "Failed to create flate writer", http.StatusInternalServerError)
				return
			}
			defer writer.Close()

			w.Header().Set("Content-Encoding", "deflate")
			dw := &deflateResponseWriter{Writer: writer, ResponseWriter: w}
			next.ServeHTTP(dw, r)

			writer.Close()
			w.Write(buf.Bytes())
		} else {
			// 不支持压缩，直接处理请求
			next.ServeHTTP(w, r)
		}
	})
}

// 处理预检请求
func handlePreflight(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Access-Control-Allow-Origin", "*")
	w.Header().Set("Access-Control-Allow-Methods", "GET, POST, OPTIONS, PUT, DELETE")
	w.Header().Set("Access-Control-Allow-Headers", "Content-Type, Authorization")
	w.Header().Set("Access-Control-Max-Age", "86400") // 缓存 1 天
	w.WriteHeader(http.StatusNoContent)
}

// Gzip 响应写入器
func (g *gzipResponseWriter) WriteHeader(statusCode int) {
	// 设置 Gzip 响应头
	g.ResponseWriter.Header().Set("Content-Encoding", "gzip")
	g.ResponseWriter.WriteHeader(statusCode)
}

func (g *gzipResponseWriter) Write(b []byte) (int, error) {
	// 写入 Gzip 压缩数据
	if g.gzipWriter != nil {
		return g.gzipWriter.Write(b)
	}
	return g.ResponseWriter.Write(b)
}

// Deflate 响应写入器
func (d *deflateResponseWriter) Write(b []byte) (int, error) {
	return d.Writer.Write(b)
}
