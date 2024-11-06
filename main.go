package main

import (
	"log"
	"net/http"
	"os"
	"time"

	"github.com/gorilla/mux"
)

var shanghaiLocation *time.Location

func main() {
	// // 读取配置文件
	// err := loadConfig()
	// if err != nil {
	// 	log.Fatalf("无法加载配置文件: %v", err)
	// }

	// // 检查 USB 是否已连接并设置初始挂载路径
	// checkInitialUSBConnection()

	// // 确保挂载基目录存在
	// if err := os.MkdirAll(usbMountBase, 0755); err != nil {
	// 	log.Fatalf("无法创建挂载基目录: %v", err)
	// }

	// // 启动 USB 设备监听
	// go monitorUSBEvents()

	initMinio() // 初始化MinIO

	router := mux.NewRouter() // 创建路由

	// 启动 SSE 服务器
	// router.HandleFunc("/usbEvents", sseHandler)

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
