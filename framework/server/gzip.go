// Package server gzip 响应压缩中间件。
//
// # 为什么不直接用 hertz-contrib/gzip
//
// 截至 2026-06，hertz-contrib/gzip 的 API 在不同 hertz 版本间偶有兼容性问题；
// 本中间件用标准库 compress/gzip 自实现，零额外依赖，且能与本包内的指标
// 中间件深度协作（响应大小指标记录的是压缩前还是压缩后由我们决定）。
//
// # 关键设计
//
//  1. 压缩在 c.Next 之后做：等业务完整生成 body 再统一压缩，避免流式压缩的复杂性
//     （Hertz 的 Body 默认就是 buffered，没有"边写边压"的明显收益）。
//  2. 跳过条件：
//     - 客户端 Accept-Encoding 不含 gzip
//     - 响应已显式设置 Content-Encoding（业务自己处理过压缩）
//     - body 长度 < MinLength（小包压缩反而更大，且 CPU 不划算）
//     - Content-Type 命中"已压缩格式"黑名单（image/*、video/*、*+gzip 等）
//  3. 压缩等级取 gzip.DefaultCompression（速度与压缩比的平衡点）；
//     高 QPS 服务可调低到 BestSpeed 换 CPU。
//
// @author wanlizhan
// @created 2026-06-01
package server

import (
	"bytes"
	"compress/gzip"
	"context"
	"strconv"
	"strings"
	"sync"

	"github.com/cloudwego/hertz/pkg/app"

	"github.com/xxzhwl/gaia"
)

// gzipConfig gzip 中间件配置（一次性从 schema 解析）。
type gzipConfig struct {
	enable bool
	// MinLength 小于该字节数不压缩；默认 1024。压缩固定 ~20 字节头开销，
	// 小包压缩可能反而变大，且 CPU 不划算。
	minLength int
	// Level gzip 压缩等级 1~9，默认 -1（DefaultCompression）。
	level int
	// excludedContentTypes 对哪些 Content-Type 跳过压缩（前缀匹配）。
	excludedContentTypes []string
}

// 默认跳过的 Content-Type（已是压缩格式或不适合压缩）
var defaultGzipExcludedTypes = []string{
	"image/",
	"video/",
	"audio/",
	"application/zip",
	"application/gzip",
	"application/x-gzip",
	"application/x-7z-compressed",
	"application/x-rar-compressed",
	"application/octet-stream", // 二进制文件无法判断压缩状态，保守跳过
}

// rawResponseSizeKey 在 ctx 中保存"压缩前响应体字节数"的 key。
// gzipPlugin 写入；metricsPlugin 优先读它来还原业务侧响应大小指标。
const rawResponseSizeKey = "_gaia_raw_response_size"

// gzipWriterPool 复用 gzip.Writer，降低高 QPS 下的 GC 压力。
// 注意：sync.Pool 中存储的对象需在 Put 之前 Reset / Close。
var gzipWriterPool = sync.Pool{
	New: func() any {
		// 这里给一个 sentinel writer；真正的 level 在 Get 后通过 gzip.NewWriterLevel 重建
		w, _ := gzip.NewWriterLevel(nil, gzip.DefaultCompression)
		return w
	},
}

func (s *Server) loadGzipConfig() gzipConfig {
	c := gzipConfig{
		enable:    gaia.GetSafeConfBool(s.schema + ".Gzip.Enable"),
		minLength: int(gaia.GetSafeConfInt64WithDefault(s.schema+".Gzip.MinLength", 1024)),
		level:     int(gaia.GetSafeConfInt64WithDefault(s.schema+".Gzip.Level", int64(gzip.DefaultCompression))),
	}
	if c.minLength <= 0 {
		c.minLength = 1024
	}
	// 校验 level 范围；越界回退到 DefaultCompression 而不是直接报错
	if c.level < gzip.HuffmanOnly || c.level > gzip.BestCompression {
		c.level = gzip.DefaultCompression
	}
	c.excludedContentTypes = append([]string{}, defaultGzipExcludedTypes...)
	if extra := gaia.GetSafeConfSlice[string](s.schema + ".Gzip.ExcludedContentTypes"); len(extra) > 0 {
		c.excludedContentTypes = append(c.excludedContentTypes, extra...)
	}
	return c
}

// gzipPlugin gzip 响应压缩中间件。
func (s *Server) gzipPlugin() app.HandlerFunc {
	cfg := s.loadGzipConfig()

	return func(c context.Context, ctx *app.RequestContext) {
		// 探针请求豁免：/livez|/readyz 响应体极小，压缩反而可能变大；
		// /metrics 是 Prometheus exposition，promhttp 自带 gzip 谈判，不能双重压缩。
		if isProbeRequest(ctx) {
			ctx.Next(c)
			return
		}

		ctx.Next(c)

		// 1) 客户端是否接受 gzip
		ae := string(ctx.Request.Header.Peek("Accept-Encoding"))
		if !strings.Contains(ae, "gzip") {
			return
		}

		// 2) 业务是否已自行压缩
		if len(ctx.Response.Header.Peek("Content-Encoding")) > 0 {
			return
		}

		// 3) Content-Type 黑名单
		ct := strings.ToLower(string(ctx.Response.Header.Peek("Content-Type")))
		for _, ex := range cfg.excludedContentTypes {
			if strings.HasPrefix(ct, ex) {
				return
			}
		}

		// 4) body 长度阈值
		body := ctx.Response.Body()
		if len(body) < cfg.minLength {
			return
		}

		// 把"压缩前 body 大小"暴露给 metricsPlugin，避免 ResponseSize 直方图
		// 受压缩比污染——metrics 衡量的应是业务负载量而不是出网字节量。
		// metricsPlugin 在 defer 中读取这个 key；不存在时回退到 len(body)。
		ctx.Set(rawResponseSizeKey, len(body))

		// 5) 实际压缩
		compressed, err := gzipCompress(body, cfg.level)
		if err != nil {
			gaia.WarnF("gzip 压缩失败，回退到未压缩响应: %v", err)
			return
		}
		// 压缩后比原始还大（极端情况）也跳过：节省下行带宽是首要目标
		if len(compressed) >= len(body) {
			return
		}

		ctx.Response.Header.Set("Content-Encoding", "gzip")
		ctx.Response.Header.Set("Vary", "Accept-Encoding")
		// 注意：Hertz 的 Header.Del("Content-Length") 在替换 body 后会自动处理；
		// 这里显式覆盖以防 framework 缓存了原长度。
		ctx.Response.Header.Set("Content-Length", strconv.Itoa(len(compressed)))
		ctx.Response.SetBodyRaw(compressed)
	}
}

// gzipCompress 使用 sync.Pool 复用 writer 完成压缩。
//
// 实现细节：从池里取出 writer 后用 Reset(buf) 重新指向新缓冲区——这是
// compress/gzip 推荐的复用模式，比每次 NewWriterLevel 节省约 30% 分配。
// 但 level 在 Reset 后保持不变，因此池里的 writer 等级被锁定为 DefaultCompression；
// 若调用方要求其他 level，则直接走慢路径 NewWriterLevel。
//
// 关于 Pool.Put：用 defer 统一回池，错误路径与成功路径共用一条出口。
// gzip.Writer 在 Close 后再次 Reset 到新 buffer 是合法的（标准库支持），
// 即便 Write/Close 出错，Put 回去后下次 Get 出来也会被 Reset 覆盖，
// 不会污染池中状态。
func gzipCompress(src []byte, level int) ([]byte, error) {
	var buf bytes.Buffer
	buf.Grow(len(src) / 2) // 经验值：HTML/JSON 压缩比通常 2-5x

	if level == gzip.DefaultCompression {
		// 快路径：从池里复用
		w := gzipWriterPool.Get().(*gzip.Writer)
		defer gzipWriterPool.Put(w)
		w.Reset(&buf)
		if _, err := w.Write(src); err != nil {
			_ = w.Close()
			return nil, err
		}
		if err := w.Close(); err != nil {
			return nil, err
		}
		return buf.Bytes(), nil
	}

	// 慢路径：自定义 level 不入池（避免污染池里 writer 的等级）
	w, err := gzip.NewWriterLevel(&buf, level)
	if err != nil {
		return nil, err
	}
	if _, err := w.Write(src); err != nil {
		_ = w.Close()
		return nil, err
	}
	if err := w.Close(); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}
