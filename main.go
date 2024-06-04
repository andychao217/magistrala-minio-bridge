package main

import (
	"context"
	"fmt"
	"io"
	"log"
	"net/http"

	"github.com/gorilla/mux"
	"github.com/minio/minio-go/v7"
	"github.com/minio/minio-go/v7/pkg/credentials"
)

var minioClient *minio.Client

const (
	bucketName = "upload-bucket"
	endpoint   = "192.168.2.100:8000" // MinIO 服务器地址
	accessKey  = "admin"
	secretKey  = "12345678"
)

func main() {
	// 初始化 MinIO 客户端
	var err error
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

	router := mux.NewRouter()
	router.HandleFunc("/upload", uploadFile).Methods("POST")
	router.HandleFunc("/files", listFiles).Methods("GET")
	router.HandleFunc("/download/{filename}", downloadFile).Methods("GET")
	router.HandleFunc("/delete/{bucket}/{object}", deleteFileHandler).Methods("DELETE")

	// 静态文件服务
	router.PathPrefix("/").Handler(http.FileServer(http.Dir("./static/")))

	http.ListenAndServe(":8080", router)
}

func uploadFile(w http.ResponseWriter, r *http.Request) {
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

func listFiles(w http.ResponseWriter, r *http.Request) {
	objectCh := minioClient.ListObjects(context.Background(), bucketName, minio.ListObjectsOptions{Recursive: true})
	for object := range objectCh {
		if object.Err != nil {
			log.Fatalln(object.Err)
		}
		fmt.Fprintln(w, object.Key)
	}
}

func downloadFile(w http.ResponseWriter, r *http.Request) {
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
	vars := mux.Vars(r)
	bucketName := vars["bucket"]
	objectName := vars["object"]

	err := minioClient.RemoveObject(context.Background(), bucketName, objectName, minio.RemoveObjectOptions{})
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	w.WriteHeader(http.StatusNoContent)
}
