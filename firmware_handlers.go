package main

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/gabriel-vasile/mimetype"
	"github.com/minio/minio-go/v7"
)

// 处理固件上传、删除、列表的代码...// 上传固件
// getObjectKey 根据 ProductName 动态生成 objectKey
func getObjectKey(productName string) string {
	return fmt.Sprintf("firmware/%s/firmwareInfo.json", productName)
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

	currentTime := time.Now()

	// 使用预加载的时区
	if shanghaiLocation == nil {
		log.Println("shanghaiLocation 为 nil，使用 UTC 时区")
		shanghaiLocation = time.UTC
	}

	localizedTime := currentTime.In(shanghaiLocation)
	newInfo.UploadTime = localizedTime.Format("2006-01-02 15:04:05")
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

	err := r.ParseMultipartForm(100 << 20) // 限制上传文件的大小为100MB
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
			return []FirmwareInfo{}, nil
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
	decoder := json.NewDecoder(object)
	decoder.DisallowUnknownFields() // 可选：防止存在未知字段时解析失败

	err = decoder.Decode(&firmwareList)
	if err != nil {
		// 如果 JSON 内容为空，返回一个空的切片
		if err == fmt.Errorf("EOF") {
			return []FirmwareInfo{}, nil
		}
		return nil, fmt.Errorf("解析 JSON 文件失败: %v", err)
	}

	// 确保返回的切片不为 nil
	if firmwareList == nil {
		firmwareList = []FirmwareInfo{}
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
