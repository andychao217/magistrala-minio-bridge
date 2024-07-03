package main

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"math"
	"net/http"
	"os"
	"path/filepath"
	"strings"

	"github.com/gorilla/mux"
	"github.com/minio/minio-go/v7"
	"github.com/minio/minio-go/v7/pkg/credentials"
)

var minioClient *minio.Client
var bucketName string

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

func uploadFileHandler(w http.ResponseWriter, r *http.Request) {
	// 设置 CORS 头
	w.Header().Set("Access-Control-Allow-Origin", "*") // 允许所有来源，或者指定具体的来源
	w.Header().Set("Access-Control-Allow-Methods", "POST")
	w.Header().Set("Access-Control-Allow-Headers", "Content-Type, Authorization")
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

		// 使用传递的路径或默认路径
		filePath := "uploads/"
		if len(filePaths) > i {
			filePath = filePaths[i]
		}

		_, err = minioClient.PutObject(context.Background(), bucketName, filePath+fileHeader.Filename, file, fileHeader.Size, minio.PutObjectOptions{ContentType: fileHeader.Header.Get("Content-Type")})
		if err != nil {
			http.Error(w, "Error uploading file", http.StatusInternalServerError)
			return
		}
	}

	fmt.Fprintf(w, "Files uploaded successfully\n")
}

func downloadFileHandler(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Access-Control-Allow-Origin", "*") // 允许所有来源，或者指定具体的来源
	w.Header().Set("Access-Control-Allow-Methods", "POST")
	w.Header().Set("Access-Control-Allow-Headers", "Content-Type, Authorization")
	w.Header().Set("Content-Type", "application/json")
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
		log.Fatalln(err)
	}
	defer object.Close()

	io.Copy(w, object)
}

func deleteFileHandler(w http.ResponseWriter, r *http.Request) {
	// 设置 CORS 头
	w.Header().Set("Access-Control-Allow-Origin", "*") // 允许所有来源，或者指定具体的来源
	w.Header().Set("Access-Control-Allow-Methods", "DELETE")
	w.Header().Set("Access-Control-Allow-Headers", "Content-Type, Authorization")
	w.Header().Set("Content-Type", "application/json")
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
		err := minioClient.RemoveObject(context.Background(), bucketName, objectName, minio.RemoveObjectOptions{})
		if err != nil {
			http.Error(w, fmt.Sprintf("Failed to delete object %s: %s", objectName, err.Error()), http.StatusInternalServerError)
			return
		}
	}

	w.WriteHeader(http.StatusNoContent)
}

type ObjectInfo struct {
	FileName     string  `json:"fileName"`
	Key          string  `json:"key"`
	IsDir        bool    `json:"isDir"`
	Size         float64 `json:"size"`
	LastModified string  `json:"lastModified"`
}

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

// 获取文件资源树
func getResourceListHanlder(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Access-Control-Allow-Origin", "*") // 允许所有来源，或者指定具体的来源
	w.Header().Set("Access-Control-Allow-Methods", "POST")
	w.Header().Set("Access-Control-Allow-Headers", "Content-Type, Authorization")
	w.Header().Set("Content-Type", "application/json")

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

// 预览音频文件
func previewFileHandler(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Access-Control-Allow-Origin", "*") // 允许所有来源，或者指定具体的来源
	w.Header().Set("Access-Control-Allow-Methods", "GET")
	w.Header().Set("Access-Control-Allow-Headers", "Content-Type, Authorization")
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

	io.Copy(w, obj)
}

// 新建文件夹
func createFolderHandler(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Access-Control-Allow-Origin", "*") // 允许所有来源，或者指定具体的来源
	w.Header().Set("Access-Control-Allow-Methods", "POST")
	w.Header().Set("Access-Control-Allow-Headers", "Content-Type, Authorization")
	w.Header().Set("Content-Type", "application/json")
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

func main() {
	initMinio()

	router := mux.NewRouter()

	router.HandleFunc("/upload", uploadFileHandler)
	router.HandleFunc("/download", downloadFileHandler)
	router.HandleFunc("/delete", deleteFileHandler)
	router.HandleFunc("/resourceList", getResourceListHanlder)
	router.HandleFunc("/previewFile", previewFileHandler)
	router.HandleFunc("/createFolder", createFolderHandler)

	// 静态文件服务
	router.PathPrefix("/").Handler(http.FileServer(http.Dir("/static")))

	port := os.Getenv("MINIO_BRIDGE_PORT")
	if port == "" {
		port = "9102" // 默认端口
	}
	log.Fatal(http.ListenAndServe(":"+port, router))
}
