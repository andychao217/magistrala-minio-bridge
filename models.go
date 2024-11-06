package main

import (
	"compress/gzip"
	"io"
	"net/http"
)

// 定义文件信息结构体
type ObjectInfo struct {
	FileName     string  `json:"fileName"`     // 文件名
	Key          string  `json:"key"`          // 文件路径
	IsDir        bool    `json:"isDir"`        // 是否为目录
	Size         float64 `json:"size"`         // 文件大小，单位为 MB
	LastModified string  `json:"lastModified"` // 上次修改时间
}

type gzipResponseWriter struct {
	io.Writer
	http.ResponseWriter
	gzipWriter *gzip.Writer
}

type deflateResponseWriter struct {
	io.Writer
	http.ResponseWriter
}

type FirmwareInfo struct {
	ID          string `json:"id"`
	ProductName string `json:"product_name"`
	Version     string `json:"version"`
	UploadUser  string `json:"upload_user"`
	UploadTime  string `json:"upload_time"`
	URL         string `json:"url"`
}

type LatestFirmware struct {
	ProductName   string `json:"product_name"`
	NewestVersion string `json:"newest_version"`
}
