package main

import (
	"bytes"
	"compress/flate"
	"compress/gzip"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"math"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/gabriel-vasile/mimetype"
	"github.com/gorilla/mux"
	"github.com/minio/minio-go/v7"
	"github.com/minio/minio-go/v7/pkg/credentials"
)

var minioClient *minio.Client // MinIO 客户端
var bucketName string         // 存储桶名称

// 初始化 MinIO 客户端
func initMinio() {
	// 初始化 MinIO 客户端
	var err error
	endpoint := os.Getenv("MINIO_ENDPOINT")
	if endpoint == "" {
		endpoint = "minio:9100"
	}
	accessKey := os.Getenv("MINIO_ACCESS_KEY")
	if accessKey == "" {
		accessKey = "admin"
	}
	secretKey := os.Getenv("MINIO_SECRET_KEY")
	if secretKey == "" {
		secretKey = "12345678"
	}
	bucketName = os.Getenv("MINIO_BUCKET_NAME")
	if bucketName == "" {
		bucketName = "nxt-tenant"
	}

	minioClient, err = minio.New(endpoint, &minio.Options{
		Creds:  credentials.NewStaticV4(accessKey, secretKey, ""),
		Secure: false,
	})

	if err != nil {
		log.Fatalln(err)
	}

	// 确保 bucket 存在
	err = minioClient.MakeBucket(context.Background(), bucketName, minio.MakeBucketOptions{})
	if err != nil {
		exists, errBucketExists := minioClient.BucketExists(context.Background(), bucketName)
		if errBucketExists == nil && exists {
			fmt.Printf("We already own %s\n", bucketName)
		} else {
			log.Fatalln(err)
		}
	}
}

// 验证文件是否是有效的文件类型
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

// 上传文件
func uploadFileHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method == http.MethodOptions {
		handlePreflight(w, r)
		return
	}

	// 设置 CORS 头
	w.Header().Set("Access-Control-Allow-Origin", "*") // 允许所有来源，或者指定具体的来源
	w.Header().Set("Access-Control-Allow-Methods", "POST")
	w.Header().Set("Access-Control-Allow-Headers", "Content-Type, Authorization")

	// 验证请求头中是否有 Authorization
	// authHeader := r.Header.Get("Authorization")
	// if authHeader == "" {
	// 	// 如果没有 Authorization 头，则返回 401 未授权错误
	// 	http.Error(w, "Authorization header missing", http.StatusUnauthorized)
	// 	return
	// }

	w.WriteHeader(http.StatusOK)

	if r.Method != http.MethodPost {
		http.Error(w, "Invalid request method", http.StatusMethodNotAllowed)
		return
	}

	err := r.ParseMultipartForm(50 << 20) // 限制上传文件的大小为50MB
	if err != nil {
		http.Error(w, "Error parsing form data", http.StatusBadRequest)
		return
	}

	files := r.MultipartForm.File["files"]
	filePaths := r.MultipartForm.Value["filePath"]

	for i, fileHeader := range files {
		if !isValidFileType(fileHeader.Filename) {
			http.Error(w, "Invalid file type. Only mp3 and wav are allowed", http.StatusBadRequest)
			return
		}

		file, err := fileHeader.Open()
		if err != nil {
			http.Error(w, "Error opening file", http.StatusInternalServerError)
			return
		}
		defer file.Close()

		// 使用 mimetype 库解析文件的 MIME 类型
		mime, err := mimetype.DetectReader(file)
		if err != nil {
			http.Error(w, "Error detecting MIME type", http.StatusInternalServerError)
			return
		}
		// 重置文件读取位置
		file.Seek(0, 0)

		// 使用传递的路径或默认路径
		filePath := "uploads/"
		if len(filePaths) > i {
			filePath = strings.TrimSuffix(filePaths[i], "/") + "/"
		}

		// 检查文件是否已存在
		_, err = minioClient.StatObject(context.Background(), bucketName, filePath+fileHeader.Filename, minio.StatObjectOptions{})
		if err == nil {
			// 文件已存在，跳过上传
			log.Printf("File %s already exists, skipping upload", filePath+fileHeader.Filename)
			continue
		} else if err.(minio.ErrorResponse).Code != "NoSuchKey" {
			// 其他错误，返回错误信息
			http.Error(w, "Error checking file existence", http.StatusInternalServerError)
			return
		}

		fmt.Println("mime 123: ", mime.String())
		// 文件不存在，上传文件
		_, err = minioClient.PutObject(context.Background(), bucketName, filePath+fileHeader.Filename, file, fileHeader.Size, minio.PutObjectOptions{ContentType: mime.String()})
		if err != nil {
			http.Error(w, "Error uploading file", http.StatusInternalServerError)
			return
		}
	}

	fmt.Fprintf(w, "Files uploaded successfully\n")
}

// 下载文件
func downloadFileHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method == http.MethodOptions {
		handlePreflight(w, r)
		return
	}

	w.Header().Set("Access-Control-Allow-Origin", "*") // 允许所有来源，或者指定具体的来源
	w.Header().Set("Access-Control-Allow-Methods", "POST")
	w.Header().Set("Access-Control-Allow-Headers", "Content-Type, Authorization")
	w.Header().Set("Content-Type", "application/json")

	// 验证请求头中是否有 Authorization
	// authHeader := r.Header.Get("Authorization")
	// if authHeader == "" {
	// 	// 如果没有 Authorization 头，则返回 401 未授权错误
	// 	http.Error(w, "Authorization header missing", http.StatusUnauthorized)
	// 	return
	// }

	w.WriteHeader(http.StatusOK)

	if r.Method != http.MethodPost {
		http.Error(w, "Invalid request method", http.StatusMethodNotAllowed)
		return
	}

	type downloadFileRequest struct {
		Key string `json:"key"`
	}

	var request downloadFileRequest
	err := json.NewDecoder(r.Body).Decode(&request)
	if err != nil {
		http.Error(w, "Invalid request body", http.StatusBadRequest)
		return
	}

	object, err := minioClient.GetObject(context.Background(), bucketName, request.Key, minio.GetObjectOptions{})
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	defer object.Close()

	// 这里可以根据文件类型决定 Content-Type
	w.Header().Set("Content-Type", "application/octet-stream")                 // 或者根据具体的文件类型设置
	w.Header().Set("Content-Disposition", "attachment; filename="+request.Key) // 触发下载

	// 读取文件内容并将其写入响应体
	w.WriteHeader(http.StatusOK) // 确保所有头信息已设置完毕
	io.Copy(w, object)
}

// DeleteDirectory 递归删除指定路径下的所有对象
func DeleteDirectory(ctx context.Context, bucketName, prefix string) error {
	// 列出指定前缀下的所有对象
	objectsCh := make(chan minio.ObjectInfo)

	go func() {
		defer close(objectsCh)
		// 列出所有对象
		opts := minio.ListObjectsOptions{
			Prefix:    prefix,
			Recursive: true,
		}
		for object := range minioClient.ListObjects(ctx, bucketName, opts) {
			if object.Err != nil {
				log.Println("Error listing objects:", object.Err)
				return
			}
			objectsCh <- object
		}
	}()

	// 删除所有对象
	for object := range objectsCh {
		err := minioClient.RemoveObject(ctx, bucketName, object.Key, minio.RemoveObjectOptions{})
		if err != nil {
			return fmt.Errorf("failed to remove object %s: %w", object.Key, err)
		}
		log.Printf("Deleted object: %s\n", object.Key)
	}

	return nil
}

// 删除文件
func deleteFileHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method == http.MethodOptions {
		handlePreflight(w, r)
		return
	}

	// 设置 CORS 头
	w.Header().Set("Access-Control-Allow-Origin", "*") // 允许所有来源，或者指定具体的来源
	w.Header().Set("Access-Control-Allow-Methods", "DELETE")
	w.Header().Set("Access-Control-Allow-Headers", "Content-Type, Authorization")
	w.Header().Set("Content-Type", "application/json")

	// 验证请求头中是否有 Authorization
	// authHeader := r.Header.Get("Authorization")
	// if authHeader == "" {
	// 	// 如果没有 Authorization 头，则返回 401 未授权错误
	// 	http.Error(w, "Authorization header missing", http.StatusUnauthorized)
	// 	return
	// }

	w.WriteHeader(http.StatusOK)

	if r.Method != http.MethodDelete {
		http.Error(w, "Invalid request method", http.StatusMethodNotAllowed)
		return
	}

	// 解析 JSON 请求体
	var requestBody struct {
		KeyList []string `json:"keyList"`
	}

	err := json.NewDecoder(r.Body).Decode(&requestBody)
	if err != nil {
		http.Error(w, "Invalid JSON body", http.StatusBadRequest)
		return
	}

	// 批量删除对象
	for _, objectName := range requestBody.KeyList {
		err := DeleteDirectory(context.Background(), bucketName, objectName)
		if err != nil {
			http.Error(w, fmt.Sprintf("Failed to delete object %s: %s", objectName, err.Error()), http.StatusInternalServerError)
			return
		}
	}

	w.WriteHeader(http.StatusNoContent)
}

// 定义文件信息结构体
type ObjectInfo struct {
	FileName     string  `json:"fileName"`     // 文件名
	Key          string  `json:"key"`          // 文件路径
	IsDir        bool    `json:"isDir"`        // 是否为目录
	Size         float64 `json:"size"`         // 文件大小，单位为 MB
	LastModified string  `json:"lastModified"` // 上次修改时间
}

// 生成文件资源列表
func buildResourceList(client *minio.Client, bucket, prefix string) []ObjectInfo {
	opts := minio.ListObjectsOptions{
		Recursive: false,
		Prefix:    prefix,
	}

	objectCh := client.ListObjects(context.Background(), bucket, opts)

	// 用于存储目录和文件的切片
	var dirs []ObjectInfo
	var files []ObjectInfo

	for object := range objectCh {
		if object.Err != nil {
			log.Println(object.Err)
			continue
		}

		// 检查是否为目录
		isDir := strings.HasSuffix(object.Key, "/")
		name := strings.TrimPrefix(object.Key, prefix)

		if isDir {
			name = strings.TrimSuffix(name, "/")
		}

		// 过滤掉 fileName 为空的数据
		if name == "" {
			continue
		}

		// 添加文件或目录到相应的切片
		sizeMB := float64(object.Size) / (1024 * 1024) // 转换为MB
		info := ObjectInfo{
			FileName:     name,
			Key:          object.Key,
			IsDir:        isDir,
			Size:         math.Round(sizeMB*100) / 100, // 保留两位小数
			LastModified: object.LastModified.Format("2006-01-02 15:04:05"),
		}

		if isDir {
			dirs = append(dirs, info)
		} else {
			files = append(files, info)
		}
	}

	// 合并目录和文件，确保目录在前
	items := append(dirs, files...)

	return items
}

// 获取文件资源列表
func getResourceListHanlder(w http.ResponseWriter, r *http.Request) {
	if r.Method == http.MethodOptions {
		handlePreflight(w, r)
		return
	}

	w.Header().Set("Access-Control-Allow-Origin", "*") // 允许所有来源，或者指定具体的来源
	w.Header().Set("Access-Control-Allow-Methods", "POST")
	w.Header().Set("Access-Control-Allow-Headers", "Content-Type, Authorization")
	w.Header().Set("Content-Type", "application/json")

	// 验证请求头中是否有 Authorization
	// authHeader := r.Header.Get("Authorization")
	// if authHeader == "" {
	// 	// 如果没有 Authorization 头，则返回 401 未授权错误
	// 	http.Error(w, "Authorization header missing", http.StatusUnauthorized)
	// 	return
	// }

	w.WriteHeader(http.StatusOK)

	if r.Method != http.MethodPost {
		http.Error(w, "Invalid request method", http.StatusMethodNotAllowed)
		return
	}

	type GetResourceListRequest struct {
		Path  string `json:"path"`
		ComID string `json:"comID"`
	}

	var request GetResourceListRequest
	err := json.NewDecoder(r.Body).Decode(&request)
	if err != nil {
		http.Error(w, "Invalid request body", http.StatusBadRequest)
		return
	}

	// 假设comID用于构建bucket名称和路径
	var prefix string
	if request.Path == "" {
		prefix = request.ComID + "/resource/"
	} else {
		prefix = request.Path
	}

	// 构建资源列表
	resourceList := buildResourceList(minioClient, bucketName, prefix)

	// 将树形结构转换为JSON格式
	jsonTree, err := json.MarshalIndent(resourceList, "", "  ")
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	// 发送JSON响应
	w.Header().Set("Content-Type", "application/json")
	w.Write(jsonTree)
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

// 预览音频文件
func previewFileHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method == http.MethodOptions {
		handlePreflight(w, r)
		return
	}

	w.Header().Set("Access-Control-Allow-Origin", "*") // 允许所有来源，或者指定具体的来源
	w.Header().Set("Access-Control-Allow-Methods", "GET")
	w.Header().Set("Access-Control-Allow-Headers", "Content-Type, Authorization")

	// 验证请求头中是否有 Authorization
	// authHeader := r.Header.Get("Authorization")
	// if authHeader == "" {
	// 	// 如果没有 Authorization 头，则返回 401 未授权错误
	// 	http.Error(w, "Authorization header missing", http.StatusUnauthorized)
	// 	return
	// }

	w.WriteHeader(http.StatusOK)

	if r.Method != http.MethodGet {
		http.Error(w, "Invalid request method", http.StatusMethodNotAllowed)
		return
	}

	key := r.URL.Query().Get("key")
	if key == "" {
		http.Error(w, "Key is required", http.StatusBadRequest)
		return
	}

	obj, err := minioClient.GetObject(context.Background(), bucketName, key, minio.GetObjectOptions{})
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	defer obj.Close()

	// 读取部分文件数据用于 MIME 类型检测
	buffer := make([]byte, 512) // 512 bytes is generally enough to detect the MIME type
	n, err := obj.Read(buffer)
	if err != nil && err != io.EOF {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	// 检测文件的 MIME 类型
	mime := mimetype.Detect(buffer[:n])
	contentType := mime.String()

	// 将文件指针重置到开头
	obj.Seek(0, io.SeekStart)

	// 设置Content-Type头
	w.Header().Set("Content-Type", "audio/mpeg")

	// 设置Content-Disposition头，如果需要让文件在浏览器中显示则设置为inline，否则为attachment
	if shouldInline(contentType) {
		w.Header().Set("Content-Disposition", "inline; filename=\""+key+"\"")
	} else {
		w.Header().Set("Content-Disposition", "attachment; filename=\""+key+"\"")
	}

	// 直接写文件内容到响应体中
	w.WriteHeader(http.StatusOK) // 确保所有头信息已设置完毕
	io.Copy(w, obj)
}

// 新建文件夹
func createFolderHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method == http.MethodOptions {
		handlePreflight(w, r)
		return
	}

	w.Header().Set("Access-Control-Allow-Origin", "*") // 允许所有来源，或者指定具体的来源
	w.Header().Set("Access-Control-Allow-Methods", "POST")
	w.Header().Set("Access-Control-Allow-Headers", "Content-Type, Authorization")
	w.Header().Set("Content-Type", "application/json")

	// 验证请求头中是否有 Authorization
	// authHeader := r.Header.Get("Authorization")
	// if authHeader == "" {
	// 	// 如果没有 Authorization 头，则返回 401 未授权错误
	// 	http.Error(w, "Authorization header missing", http.StatusUnauthorized)
	// 	return
	// }

	w.WriteHeader(http.StatusOK)

	if r.Method != http.MethodPost {
		http.Error(w, "Invalid request method", http.StatusMethodNotAllowed)
		return
	}

	type CreateFolderRequest struct {
		CurrentPath string `json:"currentPath"`
		FolderName  string `json:"folderName"`
	}

	var request CreateFolderRequest
	err := json.NewDecoder(r.Body).Decode(&request)
	if err != nil {
		http.Error(w, "Invalid request body", http.StatusBadRequest)
		return
	}

	currentPath := strings.TrimSuffix(request.CurrentPath, "/") + "/"
	folderName := strings.TrimSuffix(request.FolderName, "/") + "/"
	objectName := currentPath + folderName

	// Create a folder by creating an empty object with a trailing slash
	_, err = minioClient.PutObject(r.Context(), bucketName, objectName, nil, 0, minio.PutObjectOptions{})
	if err != nil {
		log.Println("Failed to create folder:", err)
		http.Error(w, "Failed to create folder", http.StatusInternalServerError)
		return
	}

	response := map[string]interface{}{
		"success": true,
		"message": "Folder created successfully",
	}

	w.WriteHeader(http.StatusOK)
	json.NewEncoder(w).Encode(response)
}

type gzipResponseWriter struct {
	io.Writer
	http.ResponseWriter
	gzipWriter *gzip.Writer
}

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

type deflateResponseWriter struct {
	io.Writer
	http.ResponseWriter
}

func (d *deflateResponseWriter) Write(b []byte) (int, error) {
	return d.Writer.Write(b)
}

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

func handlePreflight(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Access-Control-Allow-Origin", "*")
	w.Header().Set("Access-Control-Allow-Methods", "GET, POST, OPTIONS, PUT, DELETE")
	w.Header().Set("Access-Control-Allow-Headers", "Content-Type, Authorization")
	w.Header().Set("Access-Control-Max-Age", "86400") // 缓存 1 天
	w.WriteHeader(http.StatusNoContent)
}

// FirmwareInfo 定义了 firmwareInfo.json 文件中的每个条目的结构
type FirmwareInfo struct {
	ID          string `json:"id"`
	ProductName string `json:"product_name"`
	Version     string `json:"version"`
	UploadUser  string `json:"upload_user"`
	UploadTime  string `json:"upload_time"`
	URL         string `json:"url"`
}

// getObjectKey 根据 ProductName 动态生成 objectKey
func getObjectKey(productName string) string {
	return fmt.Sprintf("firmware/%s/firmwareInfo.json", productName)
}

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

// appendFirmwareInfo 负责将新的 FirmwareInfo 写入 firmwareInfo.json 文件
func appendFirmwareInfo(newInfo FirmwareInfo) error {
	bucketName := "nxt-device"
	ctx := context.Background()

	// 检查文件是否存在
	exists, err := minioClient.BucketExists(ctx, bucketName)
	if err != nil {
		return fmt.Errorf("检查桶是否存在失败: %v", err)
	}
	if !exists {
		return fmt.Errorf("桶 %s 不存在", bucketName)
	}

	newInfo.UploadTime = time.Now().Format("2006-01-02 15:04:05")
	newInfo.ID = strconv.Itoa(int(time.Now().Unix()))
	newInfo.URL = fmt.Sprintf("oss://%s/firmware/%s/%s_%s.img", bucketName, newInfo.ProductName, newInfo.ProductName, newInfo.Version)

	// 根据 ProductName 动态生成 objectKey
	objectKey := getObjectKey(newInfo.ProductName)

	objectExists, err := minioClient.StatObject(ctx, bucketName, objectKey, minio.StatObjectOptions{})
	if err != nil {
		if minio.ToErrorResponse(err).Code == "NoSuchKey" {
			// 文件不存在，创建一个新的 JSON 数组
			var firmwareList []FirmwareInfo
			firmwareList = append(firmwareList, newInfo)

			data, err := json.MarshalIndent(firmwareList, "", "  ")
			if err != nil {
				return fmt.Errorf("JSON 编码失败: %v", err)
			}

			// 上传新文件
			_, err = minioClient.PutObject(ctx, bucketName, objectKey, strings.NewReader(string(data)), int64(len(data)), minio.PutObjectOptions{
				ContentType: "application/json",
			})
			if err != nil {
				return fmt.Errorf("上传新 JSON 文件失败: %v", err)
			}

			fmt.Println("firmwareInfo.json 文件已创建并添加了新的条目。")
			return nil
		}
		return fmt.Errorf("获取对象信息失败: %v", err)
	}

	if objectExists.Key == "" {
		// 文件不存在，创建新的 JSON 数组
		var firmwareList []FirmwareInfo
		firmwareList = append(firmwareList, newInfo)

		data, err := json.MarshalIndent(firmwareList, "", "  ")
		if err != nil {
			return fmt.Errorf("JSON 编码失败: %v", err)
		}

		// 上传新文件
		_, err = minioClient.PutObject(ctx, bucketName, objectKey, strings.NewReader(string(data)), int64(len(data)), minio.PutObjectOptions{
			ContentType: "application/json",
		})
		if err != nil {
			return fmt.Errorf("上传新 JSON 文件失败: %v", err)
		}

		fmt.Println("firmwareInfo.json 文件已创建并添加了新的条目。")
		return nil
	}

	// 文件存在，下载并解析内容
	object, err := minioClient.GetObject(ctx, bucketName, objectKey, minio.GetObjectOptions{})
	if err != nil {
		return fmt.Errorf("下载对象失败: %v", err)
	}
	defer object.Close()

	var firmwareList []FirmwareInfo
	err = json.NewDecoder(object).Decode(&firmwareList)
	if err != nil {
		return fmt.Errorf("解析 JSON 文件失败: %v", err)
	}

	// 检查是否已存在相同的 product_name 和 version
	for _, firmware := range firmwareList {
		if firmware.ProductName == newInfo.ProductName && firmware.Version == newInfo.Version {
			fmt.Println("相同的 product_name 和 version 已存在，未执行任何操作。")
			return nil
		}
	}

	// 如果不存在，则添加新的条目
	firmwareList = append(firmwareList, newInfo)

	data, err := json.MarshalIndent(firmwareList, "", "  ")
	if err != nil {
		return fmt.Errorf("JSON 编码失败: %v", err)
	}

	// 上传更新后的文件
	_, err = minioClient.PutObject(ctx, bucketName, objectKey, strings.NewReader(string(data)), int64(len(data)), minio.PutObjectOptions{
		ContentType: "application/json",
	})
	if err != nil {
		return fmt.Errorf("上传更新后的 JSON 文件失败: %v", err)
	}

	fmt.Println("firmwareInfo.json 文件已更新并添加了新的条目。")
	return nil
}

// 上传固件
func uploadFirmwareHandler(w http.ResponseWriter, r *http.Request) {
	bucketName := "nxt-device"
	if r.Method == http.MethodOptions {
		handlePreflight(w, r)
		return
	}

	// 设置 CORS 头
	w.Header().Set("Access-Control-Allow-Origin", "*") // 允许所有来源，或者指定具体的来源
	w.Header().Set("Access-Control-Allow-Methods", "POST")
	w.Header().Set("Access-Control-Allow-Headers", "Content-Type, Authorization")

	// 验证请求头中是否有 Authorization
	// authHeader := r.Header.Get("Authorization")
	// if authHeader == "" {
	// 	// 如果没有 Authorization 头，则返回 401 未授权错误
	// 	http.Error(w, "Authorization header missing", http.StatusUnauthorized)
	// 	return
	// }

	w.WriteHeader(http.StatusOK)

	if r.Method != http.MethodPost {
		http.Error(w, "Invalid request method", http.StatusMethodNotAllowed)
		return
	}

	err := r.ParseMultipartForm(50 << 20) // 限制上传文件的大小为50MB
	if err != nil {
		http.Error(w, "Error parsing form data", http.StatusBadRequest)
		return
	}

	files := r.MultipartForm.File["files"]
	productName := r.MultipartForm.Value["product_name"]
	version := r.MultipartForm.Value["version"]
	upload_user := r.MultipartForm.Value["upload_user"]

	for _, fileHeader := range files {
		if !isValidFirmwareType(fileHeader.Filename) {
			http.Error(w, "Invalid file type. Only img files are allowed", http.StatusBadRequest)
			return
		}

		file, err := fileHeader.Open()
		if err != nil {
			http.Error(w, "Error opening file", http.StatusInternalServerError)
			return
		}
		defer file.Close()

		// 使用 mimetype 库解析文件的 MIME 类型
		mime, err := mimetype.DetectReader(file)
		if err != nil {
			http.Error(w, "Error detecting MIME type", http.StatusInternalServerError)
			return
		}
		// 重置文件读取位置
		file.Seek(0, 0)

		// 使用传递的路径或默认路径
		filePath := fmt.Sprintf("firmware/%s/", productName[0])

		// 检查文件是否已存在
		_, err = minioClient.StatObject(context.Background(), bucketName, filePath+fileHeader.Filename, minio.StatObjectOptions{})
		if err == nil {
			// 文件已存在，跳过上传
			log.Printf("File %s already exists, skipping upload", filePath+fileHeader.Filename)
			continue
		} else if err.(minio.ErrorResponse).Code != "NoSuchKey" {
			// 其他错误，返回错误信息
			http.Error(w, "Error checking file existence", http.StatusInternalServerError)
			return
		}

		fmt.Println("mime 123: ", mime.String())
		// 文件不存在，上传文件
		_, err = minioClient.PutObject(context.Background(), bucketName, filePath+fileHeader.Filename, file, fileHeader.Size, minio.PutObjectOptions{ContentType: mime.String()})
		if err != nil {
			http.Error(w, "Error uploading file", http.StatusInternalServerError)
			return
		}

		var newInfo FirmwareInfo
		newInfo.ProductName = productName[0]
		newInfo.Version = version[0]
		newInfo.UploadUser = upload_user[0]
		if err = appendFirmwareInfo(newInfo); err != nil {
			http.Error(w, "Error adding firmwareInfo", http.StatusInternalServerError)
		}
	}

	fmt.Fprintf(w, "Files uploaded successfully\n")
}

// deleteFirmwareInfo 负责根据 id 和 productName 删除对应的 FirmwareInfo 对象
// 同时删除对应的固件文件：FirmwareInfo.ProductName + "_" + FirmwareInfo.Version + ".img"
func deleteFirmwareInfo(id string, productName string) error {
	bucketName := "nxt-device"
	ctx := context.Background()

	// 检查存储桶是否存在
	exists, err := minioClient.BucketExists(ctx, bucketName)
	if err != nil {
		return fmt.Errorf("检查桶是否存在失败: %v", err)
	}
	if !exists {
		return fmt.Errorf("桶 %s 不存在", bucketName)
	}

	// 根据 ProductName 动态生成 objectKey
	objectKey := getObjectKey(productName)

	// 检查 firmwareInfo.json 文件是否存在
	_, err = minioClient.StatObject(ctx, bucketName, objectKey, minio.StatObjectOptions{})
	if err != nil {
		// 如果文件不存在，则不处理
		if minio.ToErrorResponse(err).Code == "NoSuchKey" {
			fmt.Println("firmwareInfo.json 文件不存在，无需删除。")
			return nil
		}
		return fmt.Errorf("获取对象信息失败: %v", err)
	}

	// 文件存在，下载并解析内容
	object, err := minioClient.GetObject(ctx, bucketName, objectKey, minio.GetObjectOptions{})
	if err != nil {
		return fmt.Errorf("下载对象失败: %v", err)
	}
	defer object.Close()

	var firmwareList []FirmwareInfo
	err = json.NewDecoder(object).Decode(&firmwareList)
	if err != nil {
		return fmt.Errorf("解析 JSON 文件失败: %v", err)
	}

	// 找到并删除对应的 FirmwareInfo 对象
	updatedList := []FirmwareInfo{}
	var firmwareToDelete *FirmwareInfo // 用于存储要删除的 FirmwareInfo
	found := false
	for _, firmware := range firmwareList {
		if firmware.ID == id && firmware.ProductName == productName {
			found = true
			// 存储要删除的 FirmwareInfo
			firmwareCopy := firmware // 创建副本以避免引用问题
			firmwareToDelete = &firmwareCopy
			continue // 跳过要删除的条目
		}
		updatedList = append(updatedList, firmware)
	}

	if !found {
		fmt.Printf("未找到 ID 为 %s 且 ProductName 为 %s 的 FirmwareInfo 对象。\n", id, productName)
		return nil
	}

	// 如果删除后列表为空，可以选择删除文件或保留空数组
	// 这里选择更新文件为新的空数组
	if len(updatedList) == 0 {
		// 删除 firmwareInfo.json 文件
		err = minioClient.RemoveObject(ctx, bucketName, objectKey, minio.RemoveObjectOptions{})
		if err != nil {
			return fmt.Errorf("删除 firmwareInfo.json 文件失败: %v", err)
		}
		fmt.Println("firmwareInfo.json 文件已删除，因为其中不再包含任何条目。")
	} else {
		// 否则，上传更新后的列表
		data, err := json.MarshalIndent(updatedList, "", "  ")
		if err != nil {
			return fmt.Errorf("JSON 编码失败: %v", err)
		}

		// 上传更新后的文件
		_, err = minioClient.PutObject(ctx, bucketName, objectKey, strings.NewReader(string(data)), int64(len(data)), minio.PutObjectOptions{
			ContentType: "application/json",
		})
		if err != nil {
			return fmt.Errorf("上传更新后的 JSON 文件失败: %v", err)
		}

		fmt.Println("firmwareInfo.json 文件已更新并删除了指定的条目。")
	}

	// 删除对应的固件文件
	if firmwareToDelete != nil {
		// 构造固件文件的 objectKey
		imgFileName := fmt.Sprintf("%s_%s.img", firmwareToDelete.ProductName, firmwareToDelete.Version)
		imgObjectKey := fmt.Sprintf("firmware/%s/%s", firmwareToDelete.ProductName, imgFileName)

		// 删除固件文件
		err = minioClient.RemoveObject(ctx, bucketName, imgObjectKey, minio.RemoveObjectOptions{})
		if err != nil {
			// 如果固件文件不存在，则仅输出日志，不返回错误
			if minio.ToErrorResponse(err).Code == "NoSuchKey" {
				fmt.Printf("对应的固件文件 %s 不存在，已删除 JSON 条目。\n", imgObjectKey)
			} else {
				return fmt.Errorf("删除固件文件 %s 失败: %v", imgObjectKey, err)
			}
		} else {
			fmt.Printf("已删除固件文件: %s\n", imgObjectKey)
		}
	}

	return nil
}

func deleteFirmwareHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method == http.MethodOptions {
		handlePreflight(w, r)
		return
	}

	// 设置 CORS 头
	w.Header().Set("Access-Control-Allow-Origin", "*") // 允许所有来源，或者指定具体的来源
	w.Header().Set("Access-Control-Allow-Methods", "DELETE")
	w.Header().Set("Access-Control-Allow-Headers", "Content-Type, Authorization")
	w.Header().Set("Content-Type", "application/json")

	// 验证请求头中是否有 Authorization
	// authHeader := r.Header.Get("Authorization")
	// if authHeader == "" {
	// 	// 如果没有 Authorization 头，则返回 401 未授权错误
	// 	http.Error(w, "Authorization header missing", http.StatusUnauthorized)
	// 	return
	// }

	w.WriteHeader(http.StatusOK)

	if r.Method != http.MethodDelete {
		http.Error(w, "Invalid request method", http.StatusMethodNotAllowed)
		return
	}

	// 解析 JSON 请求体
	type DeleteFirmwareRequest struct {
		Id          string `json:"id"`
		ProductName string `json:"product_name"`
	}

	var request DeleteFirmwareRequest

	err := json.NewDecoder(r.Body).Decode(&request)
	if err != nil {
		http.Error(w, "Invalid JSON body", http.StatusBadRequest)
		return
	}

	err = deleteFirmwareInfo(request.Id, request.ProductName)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	w.WriteHeader(http.StatusNoContent)
}

// getFirmwareList 查询固件信息的函数
func getFirmwareList(productName string) ([]FirmwareInfo, error) {
	bucketName := "nxt-device"
	ctx := context.Background()

	// 构造 objectKey
	objectKey := getObjectKey(productName)

	// 检查 firmwareInfo.json 文件是否存在
	_, err := minioClient.StatObject(ctx, bucketName, objectKey, minio.StatObjectOptions{})
	if err != nil {
		// 如果文件不存在，返回 nil
		if minio.ToErrorResponse(err).Code == "NoSuchKey" {
			return nil, nil
		}
		// 其他错误
		return nil, fmt.Errorf("获取对象信息失败: %v", err)
	}

	// 文件存在，下载并解析内容
	object, err := minioClient.GetObject(ctx, bucketName, objectKey, minio.GetObjectOptions{})
	if err != nil {
		return nil, fmt.Errorf("下载对象失败: %v", err)
	}
	defer object.Close()

	var firmwareList []FirmwareInfo
	err = json.NewDecoder(object).Decode(&firmwareList)
	if err != nil {
		return nil, fmt.Errorf("解析 JSON 文件失败: %v", err)
	}

	return firmwareList, nil
}

func getFirmwareListHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method == http.MethodOptions {
		handlePreflight(w, r)
		return
	}

	w.Header().Set("Access-Control-Allow-Origin", "*") // 允许所有来源，或者指定具体的来源
	w.Header().Set("Access-Control-Allow-Methods", "POST")
	w.Header().Set("Access-Control-Allow-Headers", "Content-Type, Authorization")
	w.Header().Set("Content-Type", "application/json")

	// 验证请求头中是否有 Authorization
	// authHeader := r.Header.Get("Authorization")
	// if authHeader == "" {
	// 	// 如果没有 Authorization 头，则返回 401 未授权错误
	// 	http.Error(w, "Authorization header missing", http.StatusUnauthorized)
	// 	return
	// }

	w.WriteHeader(http.StatusOK)

	if r.Method != http.MethodPost {
		http.Error(w, "Invalid request method", http.StatusMethodNotAllowed)
		return
	}

	type GetFirmwareListRequest struct {
		ProductName string `json:"product_name"`
	}

	var request GetFirmwareListRequest
	err := json.NewDecoder(r.Body).Decode(&request)
	if err != nil {
		http.Error(w, "Invalid request body", http.StatusBadRequest)
		return
	}

	firmwareList, err := getFirmwareList(request.ProductName)

	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	// 将树形结构转换为JSON格式
	jsonFirmwareList, err := json.MarshalIndent(firmwareList, "", "  ")
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	// 发送JSON响应
	w.Header().Set("Content-Type", "application/json")
	w.Write(jsonFirmwareList)
}

type LatestFirmware struct {
	ProductName   string `json:"product_name"`
	NewestVersion string `json:"newest_version"`
}

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

func getLatestFirmware(productName string) (*LatestFirmware, error) {
	bucketName := "nxt-device"
	ctx := context.Background()

	// 构造文件夹前缀
	prefix := fmt.Sprintf("firmware/%s/", productName)

	// 列出所有对象，以 prefix 为前缀
	objectsCh := minioClient.ListObjects(ctx, bucketName, minio.ListObjectsOptions{
		Prefix:    prefix,
		Recursive: false, // 仅列出该目录下的对象，不递归子目录
	})

	type FirmwareFile struct {
		Version   string
		Date      string
		FullName  string
		ObjectKey string
	}

	var firmwareFiles []FirmwareFile

	// 正则表达式匹配文件名
	// 示例文件名：NXT2204_[Std]_V1.0.5_20211011.img
	regex := regexp.MustCompile(`^` + regexp.QuoteMeta(productName) + `_[^\_]+_V(\d+\.\d+\.\d+)_(\d{8})\.img$`)

	for object := range objectsCh {
		if object.Err != nil {
			return nil, fmt.Errorf("列出对象时出错: %v", object.Err)
		}

		// 只处理 .img 文件
		if !strings.HasSuffix(object.Key, ".img") {
			continue
		}

		// 提取文件名
		fileName := strings.TrimPrefix(object.Key, prefix)

		// 使用正则表达式解析文件名
		matches := regex.FindStringSubmatch(fileName)
		if len(matches) != 3 {
			// 文件名不符合预期格式，跳过
			fmt.Printf("跳过不符合格式的文件名: %s\n", fileName)
			continue
		}

		version := matches[1] // e.g., "1.0.5"
		date := matches[2]    // e.g., "20211011"

		firmwareFiles = append(firmwareFiles, FirmwareFile{
			Version:   version,
			Date:      date,
			FullName:  fileName,
			ObjectKey: object.Key,
		})
	}

	if len(firmwareFiles) == 0 {
		return nil, fmt.Errorf("未找到产品 %s 的任何 .img 文件", productName)
	}

	// 排序固件文件，版本号升序，日期升序
	sort.Slice(firmwareFiles, func(i, j int) bool {
		// 比较版本号
		vi, _ := parseVersion(firmwareFiles[i].Version)
		vj, _ := parseVersion(firmwareFiles[j].Version)

		if vi[0] != vj[0] {
			return vi[0] < vj[0]
		}
		if vi[1] != vj[1] {
			return vi[1] < vj[1]
		}
		if vi[2] != vj[2] {
			return vi[2] < vj[2]
		}

		// 版本号相同，比较日期
		return firmwareFiles[i].Date < firmwareFiles[j].Date
	})

	// 最新的固件在最后
	latest := firmwareFiles[len(firmwareFiles)-1]

	// 构造 newest_version 字段
	newestVersion := fmt.Sprintf("[Std]_V%s_%s", latest.Version, latest.Date)

	return &LatestFirmware{
		ProductName:   productName,
		NewestVersion: newestVersion,
	}, nil
}

// getLatestFirmwaresHandler 处理获取多个产品最新固件版本的 HTTP 请求
func getLatestFirmwaresHandler(w http.ResponseWriter, r *http.Request) {
	// 处理跨域预检请求
	if r.Method == http.MethodOptions {
		handlePreflight(w, r)
		return
	}

	// 设置响应头
	w.Header().Set("Access-Control-Allow-Origin", "*") // 允许所有来源，或者指定具体的来源
	w.Header().Set("Access-Control-Allow-Methods", "POST")
	w.Header().Set("Access-Control-Allow-Headers", "Content-Type, Authorization")
	w.Header().Set("Content-Type", "application/json")

	// 仅允许 POST 方法
	if r.Method != http.MethodPost {
		http.Error(w, "Invalid request method", http.StatusMethodNotAllowed)
		return
	}

	// 定义请求体结构
	type GetLatestFirmwaresRequest struct {
		ProductNameList []string `json:"product_name_list"`
	}

	// 定义响应体结构
	type GetLatestFirmwaresResponse struct {
		LatestFirmwares []LatestFirmware  `json:"latest_firmwares"`
		Errors          map[string]string `json:"errors,omitempty"` // 可选的错误信息
	}

	// 解析请求体
	var request GetLatestFirmwaresRequest
	err := json.NewDecoder(r.Body).Decode(&request)
	if err != nil {
		http.Error(w, "Invalid request body", http.StatusBadRequest)
		return
	}

	// 验证 ProductNameList 是否为空
	if len(request.ProductNameList) == 0 {
		http.Error(w, "ProductNameList is empty", http.StatusBadRequest)
		return
	}

	// 初始化响应数据
	response := GetLatestFirmwaresResponse{
		LatestFirmwares: []LatestFirmware{},
		Errors:          make(map[string]string),
	}

	// 使用 WaitGroup 和 Mutex 实现并发查询和数据安全
	var wg sync.WaitGroup
	var mu sync.Mutex

	for _, productName := range request.ProductNameList {
		wg.Add(1)
		go func(pName string) {
			defer wg.Done()
			latest, err := getLatestFirmware(pName)
			mu.Lock()
			defer mu.Unlock()
			if err != nil {
				// 记录错误信息
				response.Errors[pName] = err.Error()
			} else {
				// 添加到响应列表
				response.LatestFirmwares = append(response.LatestFirmwares, *latest)
			}
		}(productName)
	}

	// 等待所有 goroutine 完成
	wg.Wait()

	// // 检查是否有错误
	// if len(response.Errors) > 0 {
	// 	// 可以选择返回部分成功的数据和错误信息，或者根据需求调整
	// 	// 这里我们选择同时返回成功和错误的信息
	// 	// 状态码仍然为 200 OK
	// }

	// 序列化响应数据
	jsonResponse, err := json.MarshalIndent(response, "", "  ")
	if err != nil {
		http.Error(w, "Failed to serialize response", http.StatusInternalServerError)
		return
	}

	// 发送响应
	w.WriteHeader(http.StatusOK)
	w.Write(jsonResponse)
}

func main() {
	initMinio() // 初始化MinIO

	router := mux.NewRouter() // 创建路由

	// 路由-资源文件上传、下载、删除
	router.HandleFunc("/upload", uploadFileHandler)
	router.HandleFunc("/download", downloadFileHandler)
	router.HandleFunc("/delete", deleteFileHandler)
	router.HandleFunc("/resourceList", getResourceListHanlder)
	router.HandleFunc("/previewFile", previewFileHandler)
	router.HandleFunc("/createFolder", createFolderHandler)
	// 路由-固件上传、删除、列表
	router.HandleFunc("/uploadFirmware", uploadFirmwareHandler)
	router.HandleFunc("/deleteFirmware", deleteFirmwareHandler)
	router.HandleFunc("/getFirmwareList", getFirmwareListHandler)
	router.HandleFunc("/getLatestFirmwares", getLatestFirmwaresHandler)

	// 静态文件服务
	// router.PathPrefix("/").Handler(http.FileServer(http.Dir("/static")))

	port := os.Getenv("MINIO_BRIDGE_PORT")
	if port == "" {
		port = "9102" // 默认端口
	}
	log.Fatal(http.ListenAndServe(":"+port, CompressionMiddleware(router)))
}
