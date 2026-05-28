// Package cos 腾讯云COS对象存储封装
// @author wanlizhan
// @created 2025/12/26
package cos

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"sort"
	"time"

	"github.com/pkg/errors"
	"github.com/tencentyun/cos-go-sdk-v5"
)

// MultipartUploadResult 分片上传结果
type MultipartUploadResult struct {
	Location string
	Bucket   string
	Key      string
	ETag     string
}

// InitiateMultipartUpload 初始化分片上传
func (c *Client) InitiateMultipartUpload(ctx context.Context, objectName string) (string, error) {
	if ctx == nil {
		ctx = context.Background()
	}

	v, _, err := c.client.Object.InitiateMultipartUpload(ctx, objectName, nil)
	if err != nil {
		return "", errors.Wrapf(err, "初始化分片上传失败: %s", objectName)
	}

	return v.UploadID, nil
}

// UploadPart 上传分片
func (c *Client) UploadPart(ctx context.Context, objectName, uploadID string, partNumber int, data []byte) (string, error) {
	if ctx == nil {
		ctx = context.Background()
	}

	if partNumber < 1 || partNumber > 10000 {
		return "", errors.New("分片编号必须在1-10000之间")
	}

	reader := bytes.NewReader(data)
	resp, err := c.client.Object.UploadPart(
		ctx, objectName, uploadID, partNumber, reader, nil,
	)
	if err != nil {
		return "", errors.Wrapf(err, "上传分片失败: %s, partNumber: %d", objectName, partNumber)
	}

	return resp.Header.Get("ETag"), nil
}

// CompleteMultipartUpload 完成分片上传
func (c *Client) CompleteMultipartUpload(ctx context.Context, objectName, uploadID string, parts []cos.Object) error {
	if ctx == nil {
		ctx = context.Background()
	}

	if len(parts) == 0 {
		return errors.New("分片列表不能为空")
	}

	_, _, err := c.client.Object.CompleteMultipartUpload(
		ctx, objectName, uploadID, &cos.CompleteMultipartUploadOptions{
			Parts: parts,
		},
	)
	if err != nil {
		return errors.Wrapf(err, "完成分片上传失败: %s", objectName)
	}

	return nil
}

// AbortMultipartUpload 终止分片上传
func (c *Client) AbortMultipartUpload(ctx context.Context, objectName, uploadID string) error {
	if ctx == nil {
		ctx = context.Background()
	}

	_, err := c.client.Object.AbortMultipartUpload(ctx, objectName, uploadID)
	if err != nil {
		return errors.Wrapf(err, "终止分片上传失败: %s", objectName)
	}

	return nil
}

// ListUploadedParts 列出已上传的分片
func (c *Client) ListUploadedParts(ctx context.Context, objectName, uploadID string, maxParts int) ([]cos.Object, error) {
	if ctx == nil {
		ctx = context.Background()
	}

	if maxParts <= 0 {
		maxParts = 1000
	}

	// COS SDK的ListParts方法不接受maxParts参数，我们实现简单的分页逻辑
	result, _, err := c.client.Object.ListParts(ctx, objectName, uploadID, nil)
	if err != nil {
		return nil, errors.Wrapf(err, "列出已上传分片失败: %s", objectName)
	}

	var parts []cos.Object
	for i, part := range result.Parts {
		if i >= maxParts {
			break
		}
		parts = append(parts, cos.Object{
			Key:          objectName,
			PartNumber:   part.PartNumber,
			Size:         part.Size,
			ETag:         part.ETag,
			LastModified: part.LastModified,
		})
	}

	return parts, nil
}

// ListMultipartUploads 列出进行中的分片上传
func (c *Client) ListMultipartUploads(ctx context.Context, prefix string, maxUploads int) ([]cos.Object, error) {
	if ctx == nil {
		ctx = context.Background()
	}

	if maxUploads <= 0 {
		maxUploads = 1000
	}

	result, _, err := c.client.Bucket.ListMultipartUploads(ctx, &cos.ListMultipartUploadsOptions{
		Prefix: prefix,
	})
	if err != nil {
		return nil, errors.Wrapf(err, "列出进行中的分片上传失败: %s", prefix)
	}

	var uploads []cos.Object
	for i, upload := range result.Uploads {
		if i >= maxUploads {
			break
		}
		uploads = append(uploads, cos.Object{
			Key: upload.Key,
		})
	}

	return uploads, nil
}

// PutLargeObject 大文件上传（自动分片）
func (c *Client) PutLargeObject(ctx context.Context, objectName string, data []byte, partSize int64) error {
	if ctx == nil {
		ctx = context.Background()
	}

	// 如果文件小于单个分片大小，直接上传
	if int64(len(data)) <= partSize {
		return c.PutObject(ctx, objectName, data)
	}

	// 初始化分片上传
	uploadID, err := c.InitiateMultipartUpload(ctx, objectName)
	if err != nil {
		return err
	}

	// 上传分片
	var parts []cos.Object
	dataSize := int64(len(data))
	partNumber := 1

	for offset := int64(0); offset < dataSize; offset += partSize {
		end := offset + partSize
		if end > dataSize {
			end = dataSize
		}

		partData := data[offset:end]
		etag, err := c.UploadPart(ctx, objectName, uploadID, partNumber, partData)
		if err != nil {
			// 上传失败，终止分片上传
			c.AbortMultipartUpload(ctx, objectName, uploadID)
			return err
		}

		parts = append(parts, cos.Object{
			PartNumber: partNumber,
			ETag:       etag,
		})
		partNumber++
	}

	// 按 PartNumber 升序排序（COS 要求）；同步上传时虽巧合有序，
	// 但显式排序可保证未来引入并发上传时仍正确。
	sort.Slice(parts, func(i, j int) bool { return parts[i].PartNumber < parts[j].PartNumber })

	// 完成分片上传
	return c.CompleteMultipartUpload(ctx, objectName, uploadID, parts)
}

// PutLargeObjectFromReader 从Reader上传大文件（自动分片）
//
// 注意：使用 io.ReadFull 严格按 partSize 读取，避免 io.Reader 短读导致提前 break；
// 每次循环分配新 buffer，避免 buffer 复用产生数据错乱（也为未来并发上传留余地）。
func (c *Client) PutLargeObjectFromReader(ctx context.Context, objectName string, reader io.Reader, partSize int64) error {
	if ctx == nil {
		ctx = context.Background()
	}
	if partSize <= 0 {
		return errors.New("partSize 必须大于 0")
	}

	// 初始化分片上传
	uploadID, err := c.InitiateMultipartUpload(ctx, objectName)
	if err != nil {
		return err
	}

	// 上传分片
	var parts []cos.Object
	partNumber := 1

	for {
		buffer := make([]byte, partSize)
		n, readErr := io.ReadFull(reader, buffer)
		// io.ReadFull 语义：
		//   - n == len(buffer) 且 err == nil：读满
		//   - n <  len(buffer) 且 err == io.ErrUnexpectedEOF：最后一片
		//   - n == 0 且 err == io.EOF：读完，无需上传新分片
		if readErr == io.EOF {
			break
		}
		if readErr != nil && readErr != io.ErrUnexpectedEOF {
			c.AbortMultipartUpload(ctx, objectName, uploadID)
			return errors.Wrap(readErr, "读取数据失败")
		}

		etag, uploadErr := c.UploadPart(ctx, objectName, uploadID, partNumber, buffer[:n])
		if uploadErr != nil {
			c.AbortMultipartUpload(ctx, objectName, uploadID)
			return uploadErr
		}

		parts = append(parts, cos.Object{
			PartNumber: partNumber,
			ETag:       etag,
		})
		partNumber++

		// 最后一片读完后退出
		if readErr == io.ErrUnexpectedEOF {
			break
		}
	}

	if len(parts) == 0 {
		// 没有任何分片（reader 为空），中止 upload 并直接 PutObject 一个空对象
		_ = c.AbortMultipartUpload(ctx, objectName, uploadID)
		return c.PutObject(ctx, objectName, nil)
	}

	sort.Slice(parts, func(i, j int) bool { return parts[i].PartNumber < parts[j].PartNumber })

	// 完成分片上传
	return c.CompleteMultipartUpload(ctx, objectName, uploadID, parts)
}

// GetPresignedUploadURL 获取分片上传的预签名URL
func (c *Client) GetPresignedUploadURL(ctx context.Context, objectName, uploadID string, partNumber int, expired time.Duration) (string, error) {
	if ctx == nil {
		ctx = context.Background()
	}

	if expired <= 0 {
		expired = time.Hour
	}

	if partNumber < 1 || partNumber > 10000 {
		return "", errors.New("分片编号必须在1-10000之间")
	}

	// 使用标准方法生成预签名URL
	url, err := c.client.Object.GetPresignedURL(ctx, "PUT", objectName,
		c.config.SecretID, c.config.SecretKey, expired, nil)
	if err != nil {
		return "", errors.Wrapf(err, "生成分片上传预签名URL失败: %s, partNumber: %d", objectName, partNumber)
	}

	// 手动添加分片上传参数
	urlStr := url.String()
	if uploadID != "" && partNumber > 0 {
		urlStr = fmt.Sprintf("%s&uploadId=%s&partNumber=%d", urlStr, uploadID, partNumber)
	}

	return urlStr, nil
}

// IsMultipartUploadInProgress 检查分片上传是否在进行中
func (c *Client) IsMultipartUploadInProgress(ctx context.Context, objectName string) (bool, error) {
	if ctx == nil {
		ctx = context.Background()
	}

	uploads, err := c.ListMultipartUploads(ctx, objectName, 1)
	if err != nil {
		return false, err
	}

	for _, upload := range uploads {
		if upload.Key == objectName {
			return true, nil
		}
	}

	return false, nil
}
