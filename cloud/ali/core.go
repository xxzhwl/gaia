// Package ali 包注释
// @author wanlizhan
// @created 2026-02-10
package ali

import (
	"crypto/hmac"
	"crypto/sha1"
	"encoding/base64"
	"fmt"
	"net/http"
	"net/url"
	"sort"
	"strings"
	"time"

	"github.com/xxzhwl/gaia/framework/httpclient"
)

type Client struct {
	Endpoint        string // 如 dysmsapi.aliyuncs.com
	AccessKeyID     string
	AccessKeySecret string
	RegionID        string // 如 cn-hangzhou
	SecurityToken   string // 可选，用于 STS 临时凭证
}

func NewClient(endpoint, accessKeyId, accessKeySecret string) *Client {
	return &Client{
		Endpoint:        endpoint,
		AccessKeyID:     accessKeyId,
		AccessKeySecret: accessKeySecret,
	}
}

func (c *Client) WithRegion(regionID string) *Client {
	c.RegionID = regionID
	return c
}

func (c *Client) WithSecurityToken(securityToken string) *Client {
	c.SecurityToken = securityToken
	return c
}

// - 对于 RPC：params 是 query 参数（会自动签名）
// - 对于 ROA：method 为 HTTP 方法，path 为资源路径（如 /users），params 为 query，body 为 JSON body

type RPCCallArg struct {
	Params  map[string]string
	Action  string
	Version string
}

func (c *Client) RPCCall(arg RPCCallArg) ([]byte, error) {
	return c.doRPC(arg)
}

//type ROACallArg struct {
//	Params  map[string]string
//	Method  string
//	Path    string
//	Body    []byte
//	Version string
//}
//
//func (c *Client) ROACall(arg ROACallArg) ([]byte, error) {
//	return c.doROA(arg)
//}

// ------------------ RPC 风格 ------------------
func (c *Client) doRPC(arg RPCCallArg) ([]byte, error) {
	// 合并公共参数
	p := make(map[string]string)
	for k, v := range arg.Params {
		p[k] = v
	}
	p["Version"] = arg.Version
	p["AccessKeyId"] = c.AccessKeyID
	p["RegionId"] = c.RegionID
	p["Action"] = arg.Action // 必须有
	p["Format"] = "JSON"
	p["Timestamp"] = time.Now().UTC().Format("2006-01-02T15:04:05Z")
	p["SignatureMethod"] = "HMAC-SHA1"
	p["SignatureVersion"] = "1.0"
	p["SignatureNonce"] = fmt.Sprintf("%d", time.Now().UnixNano())
	if c.SecurityToken != "" {
		p["SecurityToken"] = c.SecurityToken
	}

	// 签名
	signature := c.signRPC(p)
	p["Signature"] = signature

	// 构造 URL
	u := fmt.Sprintf("https://%s/", c.Endpoint)
	cli := httpclient.NewHttpRequest(u).WithMethod(http.MethodPost)
	contentTypeHeader := make(http.Header)
	contentTypeHeader.Add("Content-Type", "application/x-www-form-urlencoded")
	cli.WithHeader(contentTypeHeader)

	// 手动编码 body（避免 + 被转成空格）
	var buf strings.Builder
	keys := make([]string, 0, len(p))
	for k := range p {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	for i, k := range keys {
		if i > 0 {
			buf.WriteByte('&')
		}
		buf.WriteString(url.QueryEscape(k))
		buf.WriteByte('=')
		buf.WriteString(url.QueryEscape(p[k]))
	}
	cli.WithBody([]byte(buf.String()))

	respBody, code, err := cli.Do()

	if err != nil {
		return nil, err
	}

	if code != http.StatusOK {
		return nil, fmt.Errorf("RPC request failed: %d %s", code, string(respBody))
	}
	return respBody, nil
}

func (c *Client) signRPC(params map[string]string) string {
	// 排序
	keys := make([]string, 0, len(params))
	for k := range params {
		keys = append(keys, k)
	}
	sort.Strings(keys)

	// 构造 canonical query
	var query strings.Builder
	for _, k := range keys {
		query.WriteString("&")
		query.WriteString(percentEncode(k))
		query.WriteString("=")
		query.WriteString(percentEncode(params[k]))
	}
	canonical := query.String()[1:]

	stringToSign := "POST&%2F&" + percentEncode(canonical)
	key := c.AccessKeySecret + "&"
	h := hmac.New(sha1.New, []byte(key))
	h.Write([]byte(stringToSign))
	return base64.StdEncoding.EncodeToString(h.Sum(nil))
}

// ------------------ ROA 风格 ------------------
//func (c *Client) doROA(arg ROACallArg) ([]byte, error) {
//	// 公共 query 参数
//	query := url.Values{}
//	query.Set("Version", arg.Version)
//	query.Set("AccessKeyId", c.AccessKeyID)
//	query.Set("Timestamp", time.Now().UTC().Format("2006-01-02T15:04:05Z"))
//	query.Set("SignatureMethod", "HMAC-SHA256")
//	query.Set("SignatureVersion", "1.0")
//	query.Set("SignatureNonce", fmt.Sprintf("%d", time.Now().UnixNano()))
//	if c.SecurityToken != "" {
//		query.Set("SecurityToken", c.SecurityToken)
//	}
//	// 添加用户 params
//	for k, v := range arg.Params {
//		query.Set(k, v)
//	}
//
//	// 构造完整 URL
//	u := fmt.Sprintf("https://%s%s?%s", c.Endpoint, arg.Path, query.Encode())
//
//	req, err := http.NewRequest(arg.Method, u, bytes.NewReader(arg.Body))
//	if err != nil {
//		return nil, err
//	}
//	if arg.Body != nil {
//		req.Header.Set("Content-Type", "application/json")
//	}
//
//	// 构造签名字符串（ROA 使用 HTTP Method + Accept + Content-MD5 + Content-Type + Date + CanonicalizedHeaders + CanonicalizedResource）
//	// 但阿里云简化版：直接对 query string 签名（和 RPC 类似，但用 SHA256，且 path 参与）
//	canonicalizedResource := arg.Path + "?" + query.Encode()
//
//	stringToSign := arg.Method + "\n" +
//		"*/*\n" + // Accept
//		"\n" + // Content-MD5（空）
//		"application/json\n" + // Content-Type（若 body 为空可设为空，但这里统一）
//		"\n" + // Date（阿里云 ROA 实际不用 Date，而是用 Timestamp）
//		canonicalizedResource
//
//	key := c.AccessKeySecret + "&"
//	h := hmac.New(sha256.New, []byte(key))
//	h.Write([]byte(stringToSign))
//	signature := base64.StdEncoding.EncodeToString(h.Sum(nil))
//
//	// 添加签名到 query
//	finalURL := u + "&Signature=" + url.QueryEscape(signature)
//
//	// 重新创建请求（含签名）
//	req2, err := http.NewRequest(arg.Method, finalURL, bytes.NewReader(arg.Body))
//	if err != nil {
//		return nil, err
//	}
//	if arg.Body != nil {
//		req2.Header.Set("Content-Type", "application/json")
//	}
//
//	resp, err := http.DefaultClient.Do(req2)
//	if err != nil {
//		return nil, err
//	}
//	defer resp.Body.Close()
//	bodyResp, _ := ioutil.ReadAll(resp.Body)
//
//	if resp.StatusCode >= 400 {
//		return nil, fmt.Errorf("ROA request failed: %d %s", resp.StatusCode, string(bodyResp))
//	}
//	return bodyResp, nil
//}

// ------------------ 工具函数 ------------------
func percentEncode(s string) string {
	// 阿里云要求 RFC3986 编码，且 ~ 不编码
	escaped := url.QueryEscape(s)
	// QueryEscape 会把 ~ 编码为 %7E，需还原
	escaped = strings.ReplaceAll(escaped, "%7E", "~")
	// 同时确保 + 被转为 %20（QueryEscape 已处理）
	return escaped
}
