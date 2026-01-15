package handler

import (
	"net/http"
	"strings"

	"zai-proxy/internal"
)

// init 在函数冷启动时执行一次
func init() {
	// 加载配置和日志
	// 注意：环境变量需在 Vercel控制台 设置
	internal.LoadConfig()
	internal.InitLogger()
	// 警告：StartVersionUpdater 被跳过，因为 Serverless 环境不支持后台常驻进程
}

// Handler 是 Vercel 的入口函数
// 它将所有流量路由到内部处理程序
func Handler(w http.ResponseWriter, r *http.Request) {
	// 简单的路径路由
	// 注意：r.URL.Path 在 Vercel 可能是完整路径
	if strings.Contains(r.URL.Path, "/v1/models") {
		internal.HandleModels(w, r)
		return
	}
	if strings.Contains(r.URL.Path, "/v1/chat/completions") {
		internal.HandleChatCompletions(w, r)
		return
	}

	// 默认 404
	http.NotFound(w, r)
}
