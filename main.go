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

func uploadFile(w http.ResponseWriter, r *http.Request) {
	// 设置 CORS 头
	w.Header().Set("Access-Control-Allow-Origin", "*") // 允许所有来源，或者指定具体的来源
	w.Header().Set("Access-Control-Allow-Methods", "POST")
	w.Header().Set("Access-Control-Allow-Headers", "Content-Type, Authorization")
	w.WriteHeader(http.StatusOK)

	if r.Method != http.MethodPost {
		http.Error(w, "Invalid request method", http.StatusMethodNotAllowed)
		return
	}

	// 如果是其他方法，则返回方法不允许的错误
	http.Error(w, "Invalid request method", http.StatusMethodNotAllowed)

	r.ParseMultipartForm(10 << 20)
	file, handler, err := r.FormFile("file")
	if err != nil {
		fmt.Println("Error Retrieving the File")
		fmt.Println(err)
		return
	}
	defer file.Close()

	_, err = minioClient.PutObject(context.Background(), bucketName, handler.Filename, file, handler.Size, minio.PutObjectOptions{ContentType: handler.Header.Get("Content-Type")})
	if err != nil {
		fmt.Println(err)
	}
	fmt.Fprintf(w, "File uploaded successfully\n")
}

func downloadFile(w http.ResponseWriter, r *http.Request) {
	// 设置 CORS 头
	w.Header().Set("Access-Control-Allow-Origin", "*") // 允许所有来源，或者指定具体的来源
	w.Header().Set("Access-Control-Allow-Methods", "GET")
	w.Header().Set("Access-Control-Allow-Headers", "Content-Type, Authorization")
	w.WriteHeader(http.StatusOK)

	if r.Method != http.MethodGet {
		http.Error(w, "Invalid request method", http.StatusMethodNotAllowed)
		return
	}

	vars := mux.Vars(r)
	filename := vars["filename"]

	object, err := minioClient.GetObject(context.Background(), bucketName, filename, minio.GetObjectOptions{})
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
	w.WriteHeader(http.StatusOK)

	if r.Method != http.MethodDelete {
		http.Error(w, "Invalid request method", http.StatusMethodNotAllowed)
		return
	}

	vars := mux.Vars(r)
	objectName := vars["object"]

	err := minioClient.RemoveObject(context.Background(), bucketName, objectName, minio.RemoveObjectOptions{})
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

type ObjectInfo struct {
	FileName     string       `json:"fileName"`
	Key          string       `json:"key"`
	IsDir        bool         `json:"isDir"`
	Size         float64      `json:"size"`
	LastModified string       `json:"lastModified"`
	Children     []ObjectInfo `json:"children,omitempty"`
	IsRoot       bool         `json:"isRoot"`
	ParentKey    string       `json:"parentKey,omitempty"`
}

func buildTree(client *minio.Client, bucket, prefix string) ObjectInfo {
	opts := minio.ListObjectsOptions{
		Recursive: false,
		Prefix:    prefix,
	}

	objectCh := client.ListObjects(context.Background(), bucket, opts)

	root := ObjectInfo{
		FileName:  "文件列表", // 移除前缀的斜杠
		Key:       prefix,
		IsDir:     strings.HasSuffix(prefix, "/"),
		Children:  []ObjectInfo{},
		IsRoot:    true,
		ParentKey: "",
	}

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
			// 递归构建子树
			child := buildTree(client, bucket, object.Key)
			child.FileName = strings.TrimSuffix(name, "/")
			child.ParentKey = prefix // 设置父节点的 Key
			dirs = append(dirs, child)
		} else {
			// 添加文件到当前目录
			sizeMB := float64(object.Size) / (1024 * 1024) // 转换为MB
			info := ObjectInfo{
				FileName:     name,
				Key:          object.Key,
				IsDir:        isDir,
				Size:         math.Round(sizeMB*100) / 100, // 保留两位小数
				LastModified: object.LastModified.Format("2006-01-02 15:04:05"),
				ParentKey:    prefix, // 设置父节点的 Key
			}
			files = append(files, info)
		}
	}

	// 合并目录和文件，确保目录在前
	root.Children = append(dirs, files...)

	return root
}

// 获取文件资源树
func getResourceTreeHanlder(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Access-Control-Allow-Origin", "*") // 允许所有来源，或者指定具体的来源
	w.Header().Set("Access-Control-Allow-Methods", "GET")
	w.Header().Set("Access-Control-Allow-Headers", "Content-Type, Authorization")
	w.WriteHeader(http.StatusOK)

	if r.Method != http.MethodGet {
		http.Error(w, "Invalid request method", http.StatusMethodNotAllowed)
		return
	}

	// 从查询参数中获取comID
	comID := r.URL.Query().Get("comID")
	if comID == "" {
		http.Error(w, "Missing comID parameter", http.StatusBadRequest)
		return
	}

	// 假设comID用于构建bucket名称和路径
	prefix := comID + "/resource/"

	// 构建树形结构
	resourceTree := buildTree(minioClient, bucketName, prefix)

	// 将树形结构转换为JSON格式
	jsonTree, err := json.MarshalIndent(resourceTree, "", "  ")
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

func main() {
	initMinio()

	router := mux.NewRouter()

	router.HandleFunc("/upload", uploadFile)
	router.HandleFunc("/download/{filename}", downloadFile)
	router.HandleFunc("/delete/{object}", deleteFileHandler)

	router.HandleFunc("/resourceTree", getResourceTreeHanlder)
	router.HandleFunc("/previewFile", previewFileHandler)

	// 静态文件服务
	router.PathPrefix("/").Handler(http.FileServer(http.Dir("/static")))

	port := os.Getenv("MINIO_BRIDGE_PORT")
	if port == "" {
		port = "9102" // 默认端口
	}
	log.Fatal(http.ListenAndServe(":"+port, router))
}
