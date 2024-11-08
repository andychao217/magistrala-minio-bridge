package main

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"math"
	"net/http"
	"strings"

	"github.com/gabriel-vasile/mimetype"
	"github.com/minio/minio-go/v7"
)

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
