package server

import (
	"github.com/cloudwego/hertz/pkg/app"
	"github.com/hertz-contrib/cors"
	"github.com/xxzhwl/gaia"
	"time"
)

func (s *Server) corsPlugin() app.HandlerFunc {
	origins := gaia.GetSafeConfSlice[string](s.schema + ".Cors.AllowOrigins")
	methods := gaia.GetSafeConfSlice[string](s.schema + ".Cors.AllowMethods")
	headers := gaia.GetSafeConfSlice[string](s.schema + ".Cors.AllowHeaders")
	maxAge := gaia.GetSafeConfInt64WithDefault(s.schema+".Cors.MaxAge", 24)
	allowFiles := gaia.GetSafeConfBool(s.schema + ".Cors.AllowFiles")
	allowWebSockets := gaia.GetSafeConfBoolWithDefault(s.schema+".Cors.AllowWebSockets", true)
	allowCredentials := gaia.GetSafeConfBoolWithDefault(s.schema+".Cors.AllowCredentials", true)
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
