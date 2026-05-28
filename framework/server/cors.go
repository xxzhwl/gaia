package server

import (
	"time"

	"github.com/cloudwego/hertz/pkg/app"
	"github.com/hertz-contrib/cors"

	"github.com/xxzhwl/gaia"
)

func (s *Server) corsPlugin() app.HandlerFunc {
	origins := gaia.GetSafeConfSlice[string](s.schema + ".Cors.AllowOrigins")
	methods := gaia.GetSafeConfSlice[string](s.schema + ".Cors.AllowMethods")
	headers := gaia.GetSafeConfSlice[string](s.schema + ".Cors.AllowHeaders")
	maxAge := gaia.GetSafeConfInt64WithDefault(s.schema+".Cors.MaxAge", 24)
	allowFiles := gaia.GetSafeConfBool(s.schema + ".Cors.AllowFiles")
	allowWebSockets := gaia.GetSafeConfBoolWithDefault(s.schema+".Cors.AllowWebSockets", true)
	allowCredentials := gaia.GetSafeConfBoolWithDefault(s.schema+".Cors.AllowCredentials", true)

	if len(origins) == 0 {
		gaia.Warn("CORS AllowOrigins 未配置，跨域请求将全部被拒绝（如需放行请显式配置 AllowOrigins）")
	}
	// 没显式配置 methods 时给个安全默认，避免预检（OPTIONS）拿不到任何 Allow-Methods 直接 403。
	if len(methods) == 0 {
		methods = []string{"GET", "POST", "PUT", "DELETE", "PATCH", "HEAD", "OPTIONS"}
	}

	return cors.New(cors.Config{
		AllowOrigins:     origins,
		AllowMethods:     methods,
		AllowCredentials: allowCredentials,
		AllowHeaders:     headers,
		MaxAge:           time.Duration(maxAge) * time.Hour,
		AllowWebSockets:  allowWebSockets,
		AllowFiles:       allowFiles,
	})
}
