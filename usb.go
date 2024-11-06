package main

import (
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"sync"
	"time"
)

const (
	systemPath   = "/data/system" // 系统盘路径
	usbMountBase = "/data/usb"    // U 盘挂载的基目录
)

var (
	clients   = make(map[chan string]bool)
	clientsMu sync.Mutex
)

func sseHandler(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.Header().Set("Access-Control-Allow-Origin", "*")

	clientChan := make(chan string)
	defer close(clientChan)

	clientsMu.Lock()
	clients[clientChan] = true
	clientsMu.Unlock()

	defer func() {
		clientsMu.Lock()
		delete(clients, clientChan)
		clientsMu.Unlock()
		close(clientChan)
	}()

	// 心跳 goroutine
	go func() {
		ticker := time.NewTicker(10 * time.Second)
		defer ticker.Stop()

		for {
			select {
			case <-ticker.C:
				// 发送心跳
				select {
				case clientChan <- "heartbeat\n\n":
				case <-time.After(time.Second): // 防止阻塞
				}
			}
		}
	}()

	// 接收消息并发送到客户端
	for msg := range clientChan {
		_, err := fmt.Fprintf(w, "data: %s\n\n", msg)
		if err != nil {
			return // 处理写入错误
		}
		flusher, ok := w.(http.Flusher)
		if ok {
			flusher.Flush()
		}
	}
}

func checkInitialUSBConnection() {
	files, err := os.ReadDir(usbMountBase)
	if err != nil {
		log.Printf("读取 USB 目录失败: %v\n", err)
		return
	}

	usbConnected := false
	for _, file := range files {
		if file.IsDir() {
			usbPath := filepath.Join(usbMountBase, file.Name())
			if curPath == usbPath {
				usbConnected = true
				curPath = usbPath
				log.Printf("检测到 USB 已连接，使用路径: %s", usbPath)
				return
			}
		}
	}

	if !usbConnected || curPath != systemPath {
		log.Printf("未检测到 USB 或配置文件路径不是 USB 路径，使用系统路径: %s", systemPath)
		curPath = systemPath
		err = updateConfig(systemPath)
		if err != nil {
			log.Printf("更新配置文件失败: %v", err)
		}
	}
}

func monitorUSBEvents() {
	for {
		time.Sleep(10 * time.Second)
		checkUSBDevices()
	}
}

func checkUSBDevices() {
	cmd := exec.Command("lsblk", "-J", "-o", "NAME,MOUNTPOINT")
	output, err := cmd.Output()
	if err != nil {
		log.Printf("检查 USB 设备失败: %v", err)
		return
	}

	var devices map[string]interface{}
	err = json.Unmarshal(output, &devices)
	if err != nil {
		log.Printf("解析 USB 设备信息失败: %v", err)
		return
	}

	usbMounted := false // 标记是否有USB设备已挂载

	for _, device := range devices["blockdevices"].([]interface{}) {
		dev := device.(map[string]interface{})
		if mountPoint, ok := dev["mountpoint"].(string); ok && mountPoint != "" {
			if mountPoint == usbMountBase {
				log.Printf("USB 设备已挂载: %s", mountPoint)
				handleUSBMount(mountPoint)
				usbMounted = true // 设置标记为 true
			}
		} else {
			log.Printf("USB 设备未挂载: %s", dev["name"].(string))
			handleUSBUnmount(dev["name"].(string))
		}
	}

	// 只有在有USB设备挂载的情况下才获取MinIO可用空间
	if !usbMounted {
		log.Printf("没有USB设备挂载，跳过获取MinIO可用空间")
		return
	}

	// 这里可以调用获取MinIO可用空间的函数
	broadcastMinioSpace()
}

func handleUSBMount(mountPoint string) {
	curPath = mountPoint
	err := updateConfig(mountPoint)
	if err != nil {
		log.Printf("更新配置文件失败: %v", err)
	}
	broadcastMessage(fmt.Sprintf("USB 挂载: %s", mountPoint))
	broadcastMinioSpace()
}

func handleUSBUnmount(deviceName string) {
	curPath = systemPath
	err := updateConfig(systemPath)
	if err != nil {
		log.Printf("更新配置文件失败: %v", err)
	}
	broadcastMessage(fmt.Sprintf("USB 卸载: %s", deviceName))
	broadcastMinioSpace()
}

func broadcastMessage(msg string) {
	clientsMu.Lock()
	defer clientsMu.Unlock()
	for clientChan := range clients {
		clientChan <- msg
	}
}

func broadcastMinioSpace() {
	// 检查路径是否存在
	if _, err := os.Stat(curPath); os.IsNotExist(err) {
		log.Printf("路径不存在: %s", curPath)
		return
	}

	output, err := exec.Command("df", "-h", curPath).Output()
	if err != nil {
		log.Printf("获取 MinIO 可用空间失败: %v", err)
		return
	}
	spaceInfo := string(output)
	broadcastMessage(fmt.Sprintf("MinIO 可用空间: %s", spaceInfo))
}
