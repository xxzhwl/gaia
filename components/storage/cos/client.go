// Package cos 腾讯云COS对象存储封装
// @author wanlizhan
// @created 2025/12/26
package cos

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"time"

	"github.com/pkg/errors"
	"github.com/tencentyun/cos-go-sdk-v5"
	"github.com/xxzhwl/gaia"
)

// Client COS客户端封装
type Client struct {
	client *cos.Client
	config *Config
}

// Config COS配置
type Config struct {
	AppID               string `json:"appId"`
	SecretID            string `json:"secretId"`
	SecretKey           string `json:"secretKey"`
	Region              string `json:"region"`
	Bucket              string `json:"bucket"`
	Scheme              string `json:"scheme"`
	UseInternalDomain   bool   `json:"useInternalDomain"`   // 是否使用内网域名
	UseGlobalAccelerate bool   `json:"useGlobalAccelerate"` // 是否使用全球内网加速域名
}

func NewFrameworkClient() (*Client, error) {
	return NewClient("Framework.Cos")
}

// NewClient 创建COS客户端
func NewClient(schema string) (*Client, error) {
	config, err := loadConfig(schema)
	if err != nil {
		return nil, errors.Wrap(err, "加载COS配置失败")
	}

	u, err := buildBaseURL(config)
	if err != nil {
		return nil, errors.Wrap(err, "构建COS URL失败")
	}

	b := &cos.BaseURL{BucketURL: u}
	client := cos.NewClient(b, &http.Client{
		Transport: &cos.AuthorizationTransport{
			SecretID:  config.SecretID,
			SecretKey: config.SecretKey,
		},
	})

	return &Client{
		client: client,
		config: config,
	}, nil
}

// loadConfig 加载配置
func loadConfig(schema string) (*Config, error) {
	config := &Config{}

	// 使用LoadConfToObjWithErr加载配置
	if err := gaia.LoadConfToObjWithErr(schema, config); err != nil {
		return nil, errors.Wrap(err, "加载COS配置失败")
	}

	// 设置默认值
	if config.Scheme == "" {
		config.Scheme = "https"
	}

	// 验证必要配置
	if config.SecretID == "" || config.SecretKey == "" || config.Region == "" {
		return nil, errors.New("COS配置不完整，缺少SecretID、SecretKey或Region")
	}

	return config, nil
}

// buildBaseURL 构建基础URL
func buildBaseURL(config *Config) (*url.URL, error) {
	// 获取完整的Bucket名称
	bucketName := config.getBucketName()

	// 根据配置选择域名类型
	var baseURL string

	if config.UseGlobalAccelerate {
		// 全球内网加速域名
		baseURL = fmt.Sprintf("%s://%s.cos-internal.accelerate.tencentcos.cn", config.Scheme, bucketName)
	} else if config.UseInternalDomain {
		// 默认内网域名（智能解析内网IP）
		baseURL = fmt.Sprintf("%s://%s.cos.%s.myqcloud.com", config.Scheme, bucketName, config.Region)
	} else {
		// 公网域名
		baseURL = fmt.Sprintf("%s://%s.cos.%s.myqcloud.com", config.Scheme, bucketName, config.Region)
	}

	return url.Parse(baseURL)
}

// PutObject 上传对象
func (c *Client) PutObject(ctx context.Context, objectName string, data []byte) error {
	if ctx == nil {
		ctx = context.Background()
	}

	reader := bytes.NewReader(data)
	_, err := c.client.Object.Put(ctx, objectName, reader, nil)
	if err != nil {
		return errors.Wrapf(err, "上传对象失败: %s", objectName)
	}
	return nil
}

// PutObjectFromFile 从文件上传对象
func (c *Client) PutObjectFromFile(ctx context.Context, objectName, filePath string) error {
	if ctx == nil {
		ctx = context.Background()
	}

	_, err := c.client.Object.PutFromFile(ctx, objectName, filePath, nil)
	if err != nil {
		return errors.Wrapf(err, "从文件上传对象失败: %s -> %s", filePath, objectName)
	}
	return nil
}

// GetObject 下载对象
func (c *Client) GetObject(ctx context.Context, objectName string) ([]byte, error) {
	if ctx == nil {
		ctx = context.Background()
	}

	resp, err := c.client.Object.Get(ctx, objectName, nil)
	if err != nil {
		return nil, errors.Wrapf(err, "下载对象失败: %s", objectName)
	}
	defer resp.Body.Close()

	data, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, errors.Wrapf(err, "读取对象内容失败: %s", objectName)
	}

	return data, nil
}

// GetObjectToFile 下载对象到文件
func (c *Client) GetObjectToFile(ctx context.Context, objectName, filePath string) error {
	if ctx == nil {
		ctx = context.Background()
	}

	_, err := c.client.Object.GetToFile(ctx, objectName, filePath, nil)
	if err != nil {
		return errors.Wrapf(err, "下载对象到文件失败: %s -> %s", objectName, filePath)
	}
	return nil
}

// DeleteObject 删除对象
func (c *Client) DeleteObject(ctx context.Context, objectName string) error {
	if ctx == nil {
		ctx = context.Background()
	}

	_, err := c.client.Object.Delete(ctx, objectName, nil)
	if err != nil {
		return errors.Wrapf(err, "删除对象失败: %s", objectName)
	}
	return nil
}

// HeadObject 获取对象元数据
func (c *Client) HeadObject(ctx context.Context, objectName string) (*cos.Response, error) {
	if ctx == nil {
		ctx = context.Background()
	}

	resp, err := c.client.Object.Head(ctx, objectName, nil)
	if err != nil {
		return nil, errors.Wrapf(err, "获取对象元数据失败: %s", objectName)
	}
	return resp, nil
}

// ObjectExists 检查对象是否存在
func (c *Client) ObjectExists(ctx context.Context, objectName string) (bool, error) {
	if ctx == nil {
		ctx = context.Background()
	}

	_, err := c.HeadObject(ctx, objectName)
	if err != nil {
		if cos.IsNotFoundError(err) {
			return false, nil
		}
		return false, errors.Wrapf(err, "检查对象存在性失败: %s", objectName)
	}
	return true, nil
}

// GetObjectURL 获取对象访问URL
func (c *Client) GetObjectURL(objectName string) string {
	return fmt.Sprintf("%s/%s", c.getBucketURL(), objectName)
}

// GetPresignedURL 获取预签名URL
func (c *Client) GetPresignedURL(ctx context.Context, objectName string, expired time.Duration) (string, error) {
	if ctx == nil {
		ctx = context.Background()
	}

	if expired <= 0 {
		expired = time.Hour
	}

	url, err := c.client.Object.GetPresignedURL(ctx, http.MethodGet, objectName,
		c.config.SecretID, c.config.SecretKey, expired, nil)
	if err != nil {
		return "", errors.Wrapf(err, "生成预签名URL失败: %s", objectName)
	}
	return url.String(), nil
}

// ListObjects 列出对象
func (c *Client) ListObjects(ctx context.Context, prefix string, maxKeys int) ([]cos.Object, error) {
	if ctx == nil {
		ctx = context.Background()
	}

	if maxKeys <= 0 {
		maxKeys = 1000
	}

	opt := &cos.BucketGetOptions{
		Prefix:  prefix,
		MaxKeys: maxKeys,
	}

	v, _, err := c.client.Bucket.Get(ctx, opt)
	if err != nil {
		return nil, errors.Wrapf(err, "列出对象失败: %s", prefix)
	}

	return v.Contents, nil
}

// CopyObject 复制对象
func (c *Client) CopyObject(ctx context.Context, srcObject, destObject string) error {
	if ctx == nil {
		ctx = context.Background()
	}

	sourceURL := fmt.Sprintf("%s/%s", c.getBucketURL(), srcObject)
	_, _, err := c.client.Object.Copy(ctx, destObject, sourceURL, nil)
	if err != nil {
		return errors.Wrapf(err, "复制对象失败: %s -> %s", srcObject, destObject)
	}
	return nil
}

// MoveObject 移动对象（复制后删除）
func (c *Client) MoveObject(ctx context.Context, srcObject, destObject string) error {
	if ctx == nil {
		ctx = context.Background()
	}

	// 先复制
	if err := c.CopyObject(ctx, srcObject, destObject); err != nil {
		return err
	}

	// 再删除原对象
	if err := c.DeleteObject(ctx, srcObject); err != nil {
		// 如果删除失败，尝试删除新复制的对象
		c.DeleteObject(ctx, destObject)
		return errors.Wrapf(err, "移动对象失败，删除原对象时出错: %s", srcObject)
	}

	return nil
}

// GetObjectSize 获取对象大小
func (c *Client) GetObjectSize(ctx context.Context, objectName string) (int64, error) {
	if ctx == nil {
		ctx = context.Background()
	}

	resp, err := c.HeadObject(ctx, objectName)
	if err != nil {
		return 0, err
	}

	return resp.ContentLength, nil
}

// GetObjectLastModified 获取对象最后修改时间
func (c *Client) GetObjectLastModified(ctx context.Context, objectName string) (time.Time, error) {
	if ctx == nil {
		ctx = context.Background()
	}

	resp, err := c.HeadObject(ctx, objectName)
	if err != nil {
		return time.Time{}, err
	}

	return time.Parse(time.RFC1123, resp.Header.Get("Last-Modified"))
}

// getBucketName 获取完整的Bucket名称
func (c *Client) getBucketName() string {
	if c.config.Bucket != "" {
		return c.config.Bucket
	}
	return fmt.Sprintf("%s-%s", c.config.Bucket, c.config.AppID)
}

// getBucketName 获取完整的Bucket名称（Config方法）
func (c *Config) getBucketName() string {
	if c.Bucket != "" {
		return c.Bucket
	}
	if c.AppID != "" {
		return fmt.Sprintf("%s-%s", c.Bucket, c.AppID)
	}
	return c.Bucket
}

// getBucketURL 获取Bucket URL
func (c *Client) getBucketURL() string {
	return c.config.getBucketURL()
}

// getBucketURL 获取Bucket URL（Config方法）
func (c *Config) getBucketURL() string {
	bucketName := c.getBucketName()

	if c.UseGlobalAccelerate {
		// 全球内网加速域名
		return fmt.Sprintf("%s://%s.cos-internal.accelerate.tencentcos.cn", c.Scheme, bucketName)
	} else if c.UseInternalDomain {
		// 默认内网域名（智能解析内网IP）
		return fmt.Sprintf("%s://%s.cos.%s.myqcloud.com", c.Scheme, bucketName, c.Region)
	} else {
		// 公网域名
		return fmt.Sprintf("%s://%s.cos.%s.myqcloud.com", c.Scheme, bucketName, c.Region)
	}
}

// GetConfig 获取配置信息
func (c *Client) GetConfig() Config {
	return *c.config
}
