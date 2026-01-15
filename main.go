package main

import (
	"net/http"

	"zai-proxy/pkg"
)

func main() {
	pkg.LoadConfig()
	pkg.InitLogger()
	pkg.StartVersionUpdater()

	http.HandleFunc("/v1/models", pkg.HandleModels)
	http.HandleFunc("/v1/chat/completions", pkg.HandleChatCompletions)

	addr := ":" + pkg.Cfg.Port
	pkg.LogInfo("Server starting on %s", addr)
	if err := http.ListenAndServe(addr, nil); err != nil {
		pkg.LogError("Server failed: %v", err)
	}
}
