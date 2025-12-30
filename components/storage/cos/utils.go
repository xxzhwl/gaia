// Package cos 腾讯云COS对象存储封装
// @author wanlizhan
// @created 2025/12/26
package cos

import (
	"fmt"
	"path/filepath"
	"strings"
	"time"
)

// UploadOptions 上传选项
type UploadOptions struct {
	ContentType   string
	ContentLength int64
	Metadata      map[string]string
	ACL           string // private, public-read, etc.
}

// DownloadOptions 下载选项
type DownloadOptions struct {
	Range           string
	IfModifiedSince *time.Time
	IfNoneMatch     string
}

// ListOptions 列表选项
type ListOptions struct {
	Prefix    string
	Delimiter string
	Marker    string
	MaxKeys   int
}

// ObjectInfo 对象信息
type ObjectInfo struct {
	Key          string
	Size         int64
	LastModified time.Time
	ETag         string
	StorageClass string
}

// GenerateObjectName 生成对象名称（带时间戳）
func GenerateObjectName(prefix, extension string) string {
	timestamp := time.Now().Format("20060102_150405")
	if prefix != "" {
		return fmt.Sprintf("%s_%s.%s", prefix, timestamp, extension)
	}
	return fmt.Sprintf("%s.%s", timestamp, extension)
}

// GenerateUniqueObjectName 生成唯一对象名称（带随机字符串）
func GenerateUniqueObjectName(prefix, extension string) string {
	timestamp := time.Now().Format("20060102_150405")
	randomStr := fmt.Sprintf("%d", time.Now().UnixNano()%1000000)

	if prefix != "" {
		return fmt.Sprintf("%s_%s_%s.%s", prefix, timestamp, randomStr, extension)
	}
	return fmt.Sprintf("%s_%s.%s", timestamp, randomStr, extension)
}

// ValidateObjectName 验证对象名称
func ValidateObjectName(objectName string) error {
	if objectName == "" {
		return fmt.Errorf("对象名称不能为空")
	}

	if strings.HasPrefix(objectName, "/") {
		return fmt.Errorf("对象名称不能以'/'开头")
	}

	if strings.Contains(objectName, "//") {
		return fmt.Errorf("对象名称不能包含连续的'/'")
	}

	// 检查文件名长度（COS限制对象名称长度最大为850）
	if len(objectName) > 850 {
		return fmt.Errorf("对象名称长度不能超过850个字符")
	}

	return nil
}

// GetFileExtension 获取文件扩展名
func GetFileExtension(filename string) string {
	ext := filepath.Ext(filename)
	if ext == "" {
		return ""
	}
	return strings.TrimPrefix(ext, ".")
}

// BuildObjectPath 构建对象路径
func BuildObjectPath(basePath, filename string) string {
	if basePath == "" {
		return filename
	}

	// 确保basePath不以/结尾，filename不以/开头
	basePath = strings.TrimSuffix(basePath, "/")
	filename = strings.TrimPrefix(filename, "/")

	return fmt.Sprintf("%s/%s", basePath, filename)
}

// IsImageFile 检查是否为图片文件
func IsImageFile(filename string) bool {
	ext := GetFileExtension(filename)
	switch strings.ToLower(ext) {
	case "jpg", "jpeg", "png", "gif", "bmp", "webp", "svg":
		return true
	default:
		return false
	}
}

// IsVideoFile 检查是否为视频文件
func IsVideoFile(filename string) bool {
	ext := GetFileExtension(filename)
	switch strings.ToLower(ext) {
	case "mp4", "avi", "mov", "wmv", "flv", "mkv", "webm":
		return true
	default:
		return false
	}
}

// IsDocumentFile 检查是否为文档文件
func IsDocumentFile(filename string) bool {
	ext := GetFileExtension(filename)
	switch strings.ToLower(ext) {
	case "pdf", "doc", "docx", "xls", "xlsx", "ppt", "pptx", "txt":
		return true
	default:
		return false
	}
}

// GetContentTypeByExtension 根据扩展名获取Content-Type
func GetContentTypeByExtension(extension string) string {
	ext := strings.ToLower(extension)

	switch ext {
	case "jpg", "jpeg":
		return "image/jpeg"
	case "png":
		return "image/png"
	case "gif":
		return "image/gif"
	case "bmp":
		return "image/bmp"
	case "webp":
		return "image/webp"
	case "svg":
		return "image/svg+xml"
	case "pdf":
		return "application/pdf"
	case "doc":
		return "application/msword"
	case "docx":
		return "application/vnd.openxmlformats-officedocument.wordprocessingml.document"
	case "xls":
		return "application/vnd.ms-excel"
	case "xlsx":
		return "application/vnd.openxmlformats-officedocument.spreadsheetml.sheet"
	case "ppt":
		return "application/vnd.ms-powerpoint"
	case "pptx":
		return "application/vnd.openxmlformats-officedocument.presentationml.presentation"
	case "txt":
		return "text/plain"
	case "json":
		return "application/json"
	case "xml":
		return "application/xml"
	case "zip":
		return "application/zip"
	case "mp4":
		return "video/mp4"
	case "avi":
		return "video/x-msvideo"
	case "mov":
		return "video/quicktime"
	default:
		return "application/octet-stream"
	}
}

// FormatFileSize 格式化文件大小
func FormatFileSize(size int64) string {
	const (
		_  = 1.0
		KB = 1024.0
		MB = KB * 1024
		GB = MB * 1024
		TB = GB * 1024
	)

	sizeFloat := float64(size)

	switch {
	case sizeFloat >= TB:
		return fmt.Sprintf("%.2f TB", sizeFloat/TB)
	case sizeFloat >= GB:
		return fmt.Sprintf("%.2f GB", sizeFloat/GB)
	case sizeFloat >= MB:
		return fmt.Sprintf("%.2f MB", sizeFloat/MB)
	case sizeFloat >= KB:
		return fmt.Sprintf("%.2f KB", sizeFloat/KB)
	default:
		return fmt.Sprintf("%d B", size)
	}
}

// CalculateOptimalPartSize 计算最优分片大小
func CalculateOptimalPartSize(fileSize int64) int64 {
	// COS分片大小限制：最小1MB，最大5GB
	const (
		minPartSize = 1 * 1024 * 1024        // 1MB
		maxPartSize = 5 * 1024 * 1024 * 1024 // 5GB
		maxParts    = 10000                  // COS最多支持10000个分片
	)

	// 如果文件小于最小分片大小，直接返回文件大小
	if fileSize <= minPartSize {
		return fileSize
	}

	// 计算最优分片大小
	partSize := fileSize / maxParts
	if partSize < minPartSize {
		partSize = minPartSize
	}

	// 确保分片大小是1MB的整数倍
	partSize = (partSize + minPartSize - 1) / minPartSize * minPartSize

	// 不能超过最大分片大小
	if partSize > maxPartSize {
		partSize = maxPartSize
	}

	return partSize
}

// ValidateUploadOptions 验证上传选项
func ValidateUploadOptions(options *UploadOptions) error {
	if options == nil {
		return nil
	}

	// 验证Content-Type
	if options.ContentType != "" {
		// 简单的Content-Type格式验证
		if !strings.Contains(options.ContentType, "/") {
			return fmt.Errorf("无效的Content-Type格式: %s", options.ContentType)
		}
	}

	// 验证ACL
	if options.ACL != "" {
		validACLs := []string{"private", "public-read", "public-read-write", "authenticated-read"}
		valid := false
		for _, acl := range validACLs {
			if options.ACL == acl {
				valid = true
				break
			}
		}
		if !valid {
			return fmt.Errorf("无效的ACL: %s", options.ACL)
		}
	}

	return nil
}
