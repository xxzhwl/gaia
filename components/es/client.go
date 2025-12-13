// Package es 注释
// @author wanlizhan
// @created 2024/5/15
package es

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"github.com/elastic/go-elasticsearch/v8"
	"github.com/elastic/go-elasticsearch/v8/esapi"

	"github.com/xxzhwl/gaia"
)

const (
	DefaultRetries = 3
)

type Client struct {
	client *elasticsearch.TypedClient
}

func NewClient(address []string, username, password string) (*Client, error) {
	config := elasticsearch.Config{
		Addresses:           address,
		Username:            username,
		Password:            password,
		CompressRequestBody: true,
		DisableRetry:        false,
		MaxRetries:          DefaultRetries,
	}
	client, err := elasticsearch.NewTypedClient(config)
	if err != nil {
		return nil, err
	}
	return &Client{client: client}, nil
}

func NewFrameWorkEs() (*Client, error) {
	address, err := gaia.GetConfStringSliceFromString("Framework.ES.Address")
	if err != nil {
		return nil, err
	}
	username := gaia.GetSafeConfString("Framework.ES.UserName")
	password := gaia.GetSafeConfString("Framework.ES.Password")
	return NewClient(address, username, password)
}

func (c *Client) GetCli() *elasticsearch.TypedClient {
	return c.client
}

func (c *Client) ExistsIndex(index string) bool {
	exists, err := c.client.Indices.Exists(index).Do(context.Background())
	if err != nil {
		gaia.Println(gaia.LogErrorLevel, err.Error())
	}
	return exists
}

func (c *Client) CreateIndex(index string) error {
	resp, err := c.client.Indices.Create(index).Do(context.Background())
	if err != nil {
		return err
	}
	if resp == nil || resp.Index != index {
		return fmt.Errorf("create index:[%s] failed", index)
	}
	return nil
}

func (c *Client) CreateDoc(index string, doc any) (string, error) {
	resp, err := c.client.Index(index).Document(doc).Do(context.Background())
	if err != nil {
		gaia.Println(gaia.LogErrorLevel, err.Error())
		return "", err
	}
	if resp == nil {
		return "", fmt.Errorf("create doc failed")
	}
	return resp.Id_, nil
}

type SearchResult struct {
	Hits struct {
		Total struct {
			Value int `json:"value"`
		} `json:"total"`
		Data []DocData `json:"hits"`
	} `json:"hits"`
}

type DocData struct {
	Id     string          `json:"_id"`
	Detail json.RawMessage `json:"_source"`
	Score  float64         `json:"_score"`
}

func (c *Client) Search(index string, query map[string]interface{}) (SearchResult, error) {
	var result SearchResult

	body, err := json.Marshal(query)
	if err != nil {
		return result, errors.New("query json marshal failed")
	}
	req := esapi.SearchRequest{
		Index: []string{index},
		Body:  strings.NewReader(string(body)),
	}

	res, err := req.Do(context.Background(), c.client)
	if err != nil {
		return SearchResult{}, err
	}
	defer res.Body.Close()

	if res.IsError() {
		return SearchResult{}, fmt.Errorf("search request failed: %s", res.Status())
	}

	err = json.NewDecoder(res.Body).Decode(&result)
	if err != nil {
		return SearchResult{}, fmt.Errorf("failed to decode search response: %s", err)
	}

	return result, nil
}

type SimpleSearchArg struct {
	Index  string   `memo:"查询索引" json:"index" require:"1"`
	Sorts  []SortKv `memo:"排序" json:"sorts"`
	From   int64    `memo:"查询起始位置" json:"from"`
	Size   int64    `memo:"查询数量" json:"size"`
	Must   []OpArg  `memo:"相当于And查询" json:"must"`
	Should []OpArg  `memo:"相当于Or查询" json:"should"`
	Not    []OpArg  `memo:"相当于Not查询"  json:"not"`
}

type SortKv struct {
	Name string `memo:"排序字段" json:"name"`
	Desc bool   `memo:"是否倒序" json:"desc"`
}

type OpArg struct {
	Key   string `memo:"条件字段，可以加keyword" json:"key"`
	Value any    `json:"value"`
	Op    string `memo:"操作符" range:"term,match,exists,wildcard,lt,lte,gt,gte" json:"op"`
}

func (c *Client) SimpleSearch(arg SimpleSearchArg) (SearchResult, error) {
	sortMap := map[string]map[string]string{}
	for _, sortInfo := range arg.Sorts {
		if sortInfo.Desc {
			sortMap[sortInfo.Name] = map[string]string{"order": "desc"}
		} else {
			sortMap[sortInfo.Name] = map[string]string{"order": "asc"}
		}
	}

	searchMap := map[string]any{
		"should":   getCondMap(arg.Should),
		"must":     getCondMap(arg.Must),
		"must_not": getCondMap(arg.Not),
	}

	search := map[string]any{
		"query": map[string]any{
			"bool": searchMap,
		},
		"from":             arg.From,
		"size":             arg.Size,
		"sort":             sortMap,
		"track_total_hits": true,
	}
	return c.Search(arg.Index, search)
}

type CommonResult[T any] struct {
	Total   int
	LogList []T
}

func FormatResult[T any](origin SearchResult) (result CommonResult[T], err error) {
	result.Total = origin.Hits.Total.Value
	result.LogList = make([]T, len(origin.Hits.Data))
	for i, datum := range origin.Hits.Data {
		var temp T
		if err = json.Unmarshal(datum.Detail, &temp); err != nil {
			return CommonResult[T]{}, err
		}
		result.LogList[i] = temp
	}
	return
}

// getCondMap 根据条件操作返回具体查询结构
func getCondMap(args []OpArg) []map[string]any {
	res := []map[string]any{}
	for _, opArg := range args {
		switch opArg.Op {
		case "lte", "lt", "gte", "gt":
			res = append(res, map[string]any{"range": map[string]any{opArg.Key: map[string]any{opArg.Op: opArg.Value}}})
		case "term":
			res = append(res, map[string]any{"term": map[string]any{opArg.Key: opArg.Value}})
		case "terms":
			res = append(res, map[string]any{"terms": map[string]any{opArg.Key: opArg.Value}})
		case "match":
			res = append(res, map[string]any{"match": map[string]any{opArg.Key: opArg.Value}})
		case "wildcard":
			res = append(res, map[string]any{"wildcard": map[string]any{opArg.Key: opArg.Value}})
		case "exists":
			res = append(res, map[string]any{"exists": map[string]any{"field": opArg.Key}})
		default:
			// 默认使用term查询
			res = append(res, map[string]any{"term": map[string]any{opArg.Key: opArg.Value}})
		}
	}
	return res
}
