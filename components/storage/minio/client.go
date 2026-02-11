// Package minio 包注释
// @author wanlizhan
// @created 2024/7/10
package minio

import (
	"bytes"
	"context"
	"fmt"
	"net/http"
	"net/url"
	"strconv"
	"time"

	"github.com/minio/minio-go/v7"
	"github.com/minio/minio-go/v7/pkg/credentials"
	"github.com/xxzhwl/gaia"
)

type Client struct {
	Cli  *minio.Client
	Core *minio.Core
}

func NewClient(schema string) (*Client, error) {
	ept := gaia.GetSafeConfString("Framework." + schema + ".Endpoint")
	un := gaia.GetSafeConfString("Framework." + schema + ".UserName")
	pwd := gaia.GetSafeConfString("Framework." + schema + ".Password")
	client, err := minio.New(ept, &minio.Options{
		Creds:  credentials.NewStaticV4(un, pwd, ""),
		Secure: false,
	})
	if err != nil {
		return nil, err
	}

	core, err := minio.NewCore(ept, &minio.Options{
		Creds:  credentials.NewStaticV4(un, pwd, ""),
		Secure: false,
	})
	if err != nil {
		return nil, err
	}
	return &Client{Cli: client, Core: core}, nil
}

func (c *Client) PutObject(bucket string, fileName string, data []byte) error {
	reader := bytes.NewReader(data)
	_, err := c.Cli.PutObject(context.Background(), bucket, fileName, reader, reader.Size(), minio.PutObjectOptions{})
	if err != nil {
		return err
	}
	return nil
}

func (c *Client) PutMultipartObject(bucket string, fileName string, data []byte) error {
	if len(data) <= 5*1024*1024 {
		return c.PutObject(bucket, fileName, data)
	} else {
		number := 1
		length := int64(len(data))
		uploadID, err := c.Core.NewMultipartUpload(context.Background(), bucket, fileName, minio.PutObjectOptions{})
		if err != nil {
			return err
		}
		for {
			if length <= 0 {
				break
			}
			u, err := c.Core.Presign(context.Background(), http.MethodPost, bucket, fileName, time.Minute*120,
				url.Values{"uploadId": []string{uploadID}, "partNumber": []string{strconv.Itoa(number)}})
			if err != nil {
				return err
			}
			fmt.Println(u)

			length -= 5 * 1024 * 1024
		}
	}
	return nil
}
