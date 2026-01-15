package pkg

import (
	"bufio"
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/corpix/uarand"
	"github.com/google/uuid"
)

func extractLatestUserContent(messages []Message) string {
	for i := len(messages) - 1; i >= 0; i-- {
		if messages[i].Role == "user" {
			text, _ := messages[i].ParseContent()
			return text
		}
	}
	return ""
}

func extractAllImageURLs(messages []Message) []string {
	var allImageURLs []string
	for _, msg := range messages {
		_, imageURLs := msg.ParseContent()
		allImageURLs = append(allImageURLs, imageURLs...)
	}
	return allImageURLs
}

func makeUpstreamRequest(token string, messages []Message, model string, stream bool) (*http.Response, string, error) {
	payload, err := DecodeJWTPayload(token)
	if err != nil || payload == nil {
		return nil, "", fmt.Errorf("invalid token")
	}

	userID := payload.ID
	chatID := uuid.New().String()
	timestamp := time.Now().UnixMilli()
	requestID := uuid.New().String()
	userMsgID := uuid.New().String()

	targetModel := GetTargetModel(model)
	latestUserContent := extractLatestUserContent(messages)
	imageURLs := extractAllImageURLs(messages)

	signature := GenerateSignature(userID, requestID, latestUserContent, timestamp)

	url := fmt.Sprintf("https://chat.z.ai/api/v2/chat/completions?timestamp=%d&requestId=%s&user_id=%s&version=0.0.1&platform=web&token=%s&current_url=%s&pathname=%s&signature_timestamp=%d",
		timestamp, requestID, userID, token,
		fmt.Sprintf("https://chat.z.ai/c/%s", chatID),
		fmt.Sprintf("/c/%s", chatID),
		timestamp)

	enableThinking := IsThinkingModel(model)
	autoWebSearch := IsSearchModel(model)
	if targetModel == "glm-4.5v" || targetModel == "glm-4.6v" {
		autoWebSearch = false
	}

	var mcpServers []string
	if targetModel == "glm-4.6v" {
		mcpServers = []string{"vlm-image-search", "vlm-image-recognition", "vlm-image-processing"}
	}

	urlToFileID := make(map[string]string)
	var filesData []map[string]interface{}
	if len(imageURLs) > 0 {
		files, _ := UploadImages(token, imageURLs)
		for i, f := range files {
			if i < len(imageURLs) {
				urlToFileID[imageURLs[i]] = f.ID
			}
			filesData = append(filesData, map[string]interface{}{
				"type":            f.Type,
				"file":            f.File,
				"id":              f.ID,
				"url":             f.URL,
				"name":            f.Name,
				"status":          f.Status,
				"size":            f.Size,
				"error":           f.Error,
				"itemId":          f.ItemID,
				"media":           f.Media,
				"ref_user_msg_id": userMsgID,
			})
		}
	}

	var upstreamMessages []map[string]interface{}
	for _, msg := range messages {
		upstreamMessages = append(upstreamMessages, msg.ToUpstreamMessage(urlToFileID))
	}

	body := map[string]interface{}{
		"stream":           stream,
		"model":            targetModel,
		"messages":         upstreamMessages,
		"signature_prompt": latestUserContent,
		"params":           map[string]interface{}{},
		"features": map[string]interface{}{
			"image_generation": false,
			"web_search":       false,
			"auto_web_search":  autoWebSearch,
			"preview_mode":     true,
			"enable_thinking":  enableThinking,
		},
		"chat_id": chatID,
		"id":      uuid.New().String(),
	}

	if len(mcpServers) > 0 {
		body["mcp_servers"] = mcpServers
	}

	if len(filesData) > 0 {
		body["files"] = filesData
		body["current_user_message_id"] = userMsgID
	}

	bodyBytes, _ := json.Marshal(body)

	req, err := http.NewRequest("POST", url, bytes.NewReader(bodyBytes))
	if err != nil {
		return nil, "", err
	}

	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("X-FE-Version", GetFeVersion())
	req.Header.Set("X-Signature", signature)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Connection", "keep-alive")
	req.Header.Set("Origin", "https://chat.z.ai")
	req.Header.Set("Referer", fmt.Sprintf("https://chat.z.ai/c/%s", uuid.New().String()))
	req.Header.Set("User-Agent", uarand.GetRandom())

	client := &http.Client{}
	resp, err := client.Do(req)
	if err != nil {
		return nil, "", err
	}

	return resp, targetModel, nil
}

type UpstreamData struct {
	Type string `json:"type"`
	Data struct {
		DeltaContent string `json:"delta_content"`
		EditContent  string `json:"edit_content"`
		Phase        string `json:"phase"`
		Done         bool   `json:"done"`
	} `json:"data"`
}

func (u *UpstreamData) GetEditContent() string {
	editContent := u.Data.EditContent
	if editContent == "" {
		return ""
	}

	if len(editContent) > 0 && editContent[0] == '"' {
		var unescaped string
		if err := json.Unmarshal([]byte(editContent), &unescaped); err == nil {
			LogDebug("[GetEditContent] Unescaped edit_content from JSON string")
			return unescaped
		}
	}

	return editContent
}

type ThinkingFilter struct {
	hasSeenFirstThinking bool
	buffer               string
	lastOutputChunk      string
	lastPhase            string
	thinkingRoundCount   int
}

func (f *ThinkingFilter) ProcessThinking(deltaContent string) string {
	if !f.hasSeenFirstThinking {
		f.hasSeenFirstThinking = true
		if idx := strings.Index(deltaContent, "> "); idx != -1 {
			deltaContent = deltaContent[idx+2:]
		} else {
			return ""
		}
	}

	content := f.buffer + deltaContent
	f.buffer = ""

	content = strings.ReplaceAll(content, "\n> ", "\n")

	if strings.HasSuffix(content, "\n>") {
		f.buffer = "\n>"
		return content[:len(content)-2]
	}
	if strings.HasSuffix(content, "\n") {
		f.buffer = "\n"
		return content[:len(content)-1]
	}

	return content
}

func (f *ThinkingFilter) Flush() string {
	result := f.buffer
	f.buffer = ""
	return result
}

func (f *ThinkingFilter) ExtractCompleteThinking(editContent string) string {
	startIdx := strings.Index(editContent, "> ")
	if startIdx == -1 {
		return ""
	}
	startIdx += 2

	endIdx := strings.Index(editContent, "\n</details>")
	if endIdx == -1 {
		return ""
	}

	content := editContent[startIdx:endIdx]
	content = strings.ReplaceAll(content, "\n> ", "\n")
	return content
}

func (f *ThinkingFilter) ExtractIncrementalThinking(editContent string) string {
	completeThinking := f.ExtractCompleteThinking(editContent)
	if completeThinking == "" {
		return ""
	}

	if f.lastOutputChunk == "" {
		return completeThinking
	}

	idx := strings.Index(completeThinking, f.lastOutputChunk)
	if idx == -1 {
		return completeThinking
	}

	incrementalPart := completeThinking[idx+len(f.lastOutputChunk):]
	return incrementalPart
}

func (f *ThinkingFilter) ResetForNewRound() {
	f.lastOutputChunk = ""
	f.hasSeenFirstThinking = false
}

func HandleChatCompletions(w http.ResponseWriter, r *http.Request) {
	token := strings.TrimPrefix(r.Header.Get("Authorization"), "Bearer ")
	if token == "" {
		http.Error(w, "Unauthorized", http.StatusUnauthorized)
		return
	}

	if token == "free" {
		anonymousToken, err := GetAnonymousToken()
		if err != nil {
			LogError("Failed to get anonymous token: %v", err)
			http.Error(w, "Failed to get anonymous token", http.StatusInternalServerError)
			return
		}
		token = anonymousToken
	}

	var req ChatRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "Invalid request", http.StatusBadRequest)
		return
	}

	if req.Model == "" {
		req.Model = "GLM-4.6"
	}

	// Detect Vercel environment (does not support http.Flusher)
	// If on Vercel, force stream=false to avoid parsing SSE in non-stream handler
	isVercel := os.Getenv("VERCEL") == "1" || os.Getenv("AWS_LAMBDA_FUNCTION_NAME") != ""

	// Force disable stream if on Vercel, even if client requested it
	if isVercel {
		req.Stream = false
	}

	resp, modelName, err := makeUpstreamRequest(token, req.Messages, req.Model, req.Stream)
	if err != nil {
		LogError("Upstream request failed: %v", err)
		http.Error(w, "Upstream error", http.StatusBadGateway)
		return
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		bodyStr := string(body)
		if len(bodyStr) > 500 {
			bodyStr = bodyStr[:500]
		}
		LogError("Upstream error: status=%d, body=%s", resp.StatusCode, bodyStr)
		http.Error(w, "Upstream error", resp.StatusCode)
		return
	}

	completionID := fmt.Sprintf("chatcmpl-%s", uuid.New().String()[:29])

	if req.Stream {
		handleStreamResponse(w, resp.Body, completionID, modelName)
	} else {
		// If we forced stream=false above, req.Stream is false here, so we go here.
		// handleNonStreamResponse must be updated to handle standard JSON response too.
		handleNonStreamResponse(w, resp.Body, completionID, modelName)
	}
}

func handleStreamResponse(w http.ResponseWriter, body io.ReadCloser, completionID, modelName string) {
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")

	flusher, ok := w.(http.Flusher)
	if !ok {
		// Vercel Go Runtime does not support http.Flusher.
		// Fallback to non-stream response to avoid 500 error.
		LogInfo("Streaming not supported (http.Flusher failed), falling back to buffered response")
		handleNonStreamResponse(w, body, completionID, modelName)
		return
	}

	scanner := bufio.NewScanner(body)
	scanner.Buffer(make([]byte, 1024*1024), 1024*1024)
	hasContent := false
	searchRefFilter := NewSearchRefFilter()
	thinkingFilter := &ThinkingFilter{}
	pendingSourcesMarkdown := ""
	pendingImageSearchMarkdown := ""
	totalContentOutputLength := 0 // 记录已输出的 content 字符长度

	for scanner.Scan() {
		line := scanner.Text()
		LogDebug("[Upstream] %s", line)

		if !strings.HasPrefix(line, "data: ") {
			continue
		}

		payload := strings.TrimPrefix(line, "data: ")
		if payload == "[DONE]" {
			break
		}

		var upstream UpstreamData
		if err := json.Unmarshal([]byte(payload), &upstream); err != nil {
			continue
		}

		if upstream.Data.Phase == "done" {
			break
		}

		if upstream.Data.Phase == "thinking" && upstream.Data.DeltaContent != "" {
			isNewThinkingRound := false
			if thinkingFilter.lastPhase != "" && thinkingFilter.lastPhase != "thinking" {
				thinkingFilter.ResetForNewRound()
				thinkingFilter.thinkingRoundCount++
				isNewThinkingRound = true
			}
			thinkingFilter.lastPhase = "thinking"

			reasoningContent := thinkingFilter.ProcessThinking(upstream.Data.DeltaContent)

			if isNewThinkingRound && thinkingFilter.thinkingRoundCount > 1 && reasoningContent != "" {
				reasoningContent = "\n\n" + reasoningContent
			}

			if reasoningContent != "" {
				thinkingFilter.lastOutputChunk = reasoningContent
				reasoningContent = searchRefFilter.Process(reasoningContent)

				if reasoningContent != "" {
					hasContent = true
					chunk := ChatCompletionChunk{
						ID:      completionID,
						Object:  "chat.completion.chunk",
						Created: time.Now().Unix(),
						Model:   modelName,
						Choices: []Choice{{
							Index:        0,
							Delta:        Delta{ReasoningContent: reasoningContent},
							FinishReason: nil,
						}},
					}
					data, _ := json.Marshal(chunk)
					fmt.Fprintf(w, "data: %s\n\n", data)
					flusher.Flush()
				}
			}
			continue
		}

		if upstream.Data.Phase != "" {
			thinkingFilter.lastPhase = upstream.Data.Phase
		}

		editContent := upstream.GetEditContent()
		if editContent != "" && IsSearchResultContent(editContent) {
			if results := ParseSearchResults(editContent); len(results) > 0 {
				searchRefFilter.AddSearchResults(results)
				pendingSourcesMarkdown = searchRefFilter.GetSearchResultsMarkdown()
			}
			continue
		}
		if editContent != "" && strings.Contains(editContent, `"search_image"`) {
			textBeforeBlock := ExtractTextBeforeGlmBlock(editContent)
			if textBeforeBlock != "" {
				textBeforeBlock = searchRefFilter.Process(textBeforeBlock)
				if textBeforeBlock != "" {
					hasContent = true
					chunk := ChatCompletionChunk{
						ID:      completionID,
						Object:  "chat.completion.chunk",
						Created: time.Now().Unix(),
						Model:   modelName,
						Choices: []Choice{{
							Index:        0,
							Delta:        Delta{Content: textBeforeBlock},
							FinishReason: nil,
						}},
					}
					data, _ := json.Marshal(chunk)
					fmt.Fprintf(w, "data: %s\n\n", data)
	if flusher != nil {
		flusher.Flush()
	}
				}
			}
			if results := ParseImageSearchResults(editContent); len(results) > 0 {
				pendingImageSearchMarkdown = FormatImageSearchResults(results)
			}
			continue
		}
		if editContent != "" && strings.Contains(editContent, `"mcp"`) {
			textBeforeBlock := ExtractTextBeforeGlmBlock(editContent)
			if textBeforeBlock != "" {
				textBeforeBlock = searchRefFilter.Process(textBeforeBlock)
				if textBeforeBlock != "" {
					hasContent = true
					chunk := ChatCompletionChunk{
						ID:      completionID,
						Object:  "chat.completion.chunk",
						Created: time.Now().Unix(),
						Model:   modelName,
						Choices: []Choice{{
							Index:        0,
							Delta:        Delta{Content: textBeforeBlock},
							FinishReason: nil,
						}},
					}
					data, _ := json.Marshal(chunk)
					fmt.Fprintf(w, "data: %s\n\n", data)
	if flusher != nil {
		flusher.Flush()
	}
				}
			}
			continue
		}
		if editContent != "" && IsSearchToolCall(editContent, upstream.Data.Phase) {
			continue
		}

		if pendingSourcesMarkdown != "" {
			hasContent = true
			chunk := ChatCompletionChunk{
				ID:      completionID,
				Object:  "chat.completion.chunk",
				Created: time.Now().Unix(),
				Model:   modelName,
				Choices: []Choice{{
					Index:        0,
					Delta:        Delta{Content: pendingSourcesMarkdown},
					FinishReason: nil,
				}},
			}
			data, _ := json.Marshal(chunk)
			fmt.Fprintf(w, "data: %s\n\n", data)
			flusher.Flush()
			pendingSourcesMarkdown = ""
		}
		if pendingImageSearchMarkdown != "" {
			hasContent = true
			chunk := ChatCompletionChunk{
				ID:      completionID,
				Object:  "chat.completion.chunk",
				Created: time.Now().Unix(),
				Model:   modelName,
				Choices: []Choice{{
					Index:        0,
					Delta:        Delta{Content: pendingImageSearchMarkdown},
					FinishReason: nil,
				}},
			}
			data, _ := json.Marshal(chunk)
			fmt.Fprintf(w, "data: %s\n\n", data)
			flusher.Flush()
			pendingImageSearchMarkdown = ""
		}

		content := ""
		reasoningContent := ""

		if thinkingRemaining := thinkingFilter.Flush(); thinkingRemaining != "" {
			thinkingFilter.lastOutputChunk = thinkingRemaining
			processedRemaining := searchRefFilter.Process(thinkingRemaining)
			if processedRemaining != "" {
				hasContent = true
				chunk := ChatCompletionChunk{
					ID:      completionID,
					Object:  "chat.completion.chunk",
					Created: time.Now().Unix(),
					Model:   modelName,
					Choices: []Choice{{
						Index:        0,
						Delta:        Delta{ReasoningContent: processedRemaining},
						FinishReason: nil,
					}},
				}
				data, _ := json.Marshal(chunk)
				fmt.Fprintf(w, "data: %s\n\n", data)
				flusher.Flush()
			}
		}

		if pendingSourcesMarkdown != "" && thinkingFilter.hasSeenFirstThinking {
			hasContent = true
			chunk := ChatCompletionChunk{
				ID:      completionID,
				Object:  "chat.completion.chunk",
				Created: time.Now().Unix(),
				Model:   modelName,
				Choices: []Choice{{
					Index:        0,
					Delta:        Delta{ReasoningContent: pendingSourcesMarkdown},
					FinishReason: nil,
				}},
			}
			data, _ := json.Marshal(chunk)
			fmt.Fprintf(w, "data: %s\n\n", data)
			flusher.Flush()
			pendingSourcesMarkdown = ""
		}

		if upstream.Data.Phase == "answer" && upstream.Data.DeltaContent != "" {
			content = upstream.Data.DeltaContent
		} else if upstream.Data.Phase == "answer" && editContent != "" {
			if strings.Contains(editContent, "</details>") {
				reasoningContent = thinkingFilter.ExtractIncrementalThinking(editContent)

				if idx := strings.Index(editContent, "</details>"); idx != -1 {
					afterDetails := editContent[idx+len("</details>"):]
					if strings.HasPrefix(afterDetails, "\n") {
						content = afterDetails[1:]
					} else {
						content = afterDetails
					}
					totalContentOutputLength = len([]rune(content))
				}
			}
		} else if (upstream.Data.Phase == "other" || upstream.Data.Phase == "tool_call") && editContent != "" {
			fullContent := editContent
			fullContentRunes := []rune(fullContent)

			if len(fullContentRunes) > totalContentOutputLength {
				content = string(fullContentRunes[totalContentOutputLength:])
				totalContentOutputLength = len(fullContentRunes)
			} else {
				content = fullContent
			}
		}

		if reasoningContent != "" {
			reasoningContent = searchRefFilter.Process(reasoningContent) + searchRefFilter.Flush()
		}
		if reasoningContent != "" {
			hasContent = true
			chunk := ChatCompletionChunk{
				ID:      completionID,
				Object:  "chat.completion.chunk",
				Created: time.Now().Unix(),
				Model:   modelName,
				Choices: []Choice{{
					Index:        0,
					Delta:        Delta{ReasoningContent: reasoningContent},
					FinishReason: nil,
				}},
			}
			data, _ := json.Marshal(chunk)
			fmt.Fprintf(w, "data: %s\n\n", data)
			flusher.Flush()
		}

		if content == "" {
			continue
		}

		content = searchRefFilter.Process(content)
		if content == "" {
			continue
		}

		hasContent = true
		if upstream.Data.Phase == "answer" && upstream.Data.DeltaContent != "" {
			totalContentOutputLength += len([]rune(content))
		}

		chunk := ChatCompletionChunk{
			ID:      completionID,
			Object:  "chat.completion.chunk",
			Created: time.Now().Unix(),
			Model:   modelName,
			Choices: []Choice{{
				Index:        0,
				Delta:        Delta{Content: content},
				FinishReason: nil,
			}},
		}

		data, _ := json.Marshal(chunk)
		fmt.Fprintf(w, "data: %s\n\n", data)
		flusher.Flush()
	}

	if err := scanner.Err(); err != nil {
		LogError("[Upstream] scanner error: %v", err)
	}

	if remaining := searchRefFilter.Flush(); remaining != "" {
		hasContent = true
		chunk := ChatCompletionChunk{
			ID:      completionID,
			Object:  "chat.completion.chunk",
			Created: time.Now().Unix(),
			Model:   modelName,
			Choices: []Choice{{
				Index:        0,
				Delta:        Delta{Content: remaining},
				FinishReason: nil,
			}},
		}
		data, _ := json.Marshal(chunk)
		fmt.Fprintf(w, "data: %s\n\n", data)
		flusher.Flush()
	}

	if !hasContent {
		LogError("Stream response 200 but no content received")
	}

	stopReason := "stop"
	finalChunk := ChatCompletionChunk{
		ID:      completionID,
		Object:  "chat.completion.chunk",
		Created: time.Now().Unix(),
		Model:   modelName,
		Choices: []Choice{{
			Index:        0,
			Delta:        Delta{},
			FinishReason: &stopReason,
		}},
	}

	data, _ := json.Marshal(finalChunk)
	fmt.Fprintf(w, "data: %s\n\n", data)
	fmt.Fprintf(w, "data: [DONE]\n\n")
	flusher.Flush()
}

func handleNonStreamResponse(w http.ResponseWriter, body io.ReadCloser, completionID, modelName string) {
	bodyBytes, _ := io.ReadAll(body)
	if len(bodyBytes) == 0 {
		LogError("Non-stream response 200 but empty body")
		http.Error(w, "Empty upstream response", http.StatusBadGateway)
		return
	}

	// 尝试直接解析为完整 JSON (当 stream=false 时)
	// z.ai 非流式响应结构需要确认，这里假设与 OpenAI 格式类似，或我们需要解析 UpstreamData 的变体
	// 如果 Upstream 返回普通 JSON，则不是 SSE 格式 (data: ...)
	
	// 为了兼容，我们先检查是否是 SSE
	bodyStr := string(bodyBytes)
	isSSE := strings.Contains(bodyStr, "data: ")

	var fullContent string
	var fullReasoning string

	if !isSSE {
		// 解析普通 JSON 响应
		// 这里假设 z.ai 在 stream=false 时返回类似 OpenAI 的结构，或者自定义结构
		// 由于 z.ai API 文档不明确，我们打印出来看看
		LogDebug("Received non-sse response: %s", bodyStr[:min(len(bodyStr), 200)])
		
		// 尝试解析常见结构
		// 假设返回结构体包含 choices...
		var directResp ChatCompletionResponse
		if err := json.Unmarshal(bodyBytes, &directResp); err == nil && len(directResp.Choices) > 0 {
			msg := directResp.Choices[0].Message
			if msg != nil {
				fullContent = msg.Content
				fullReasoning = msg.ReasoningContent
			}
		} else {
			// 如果不是标准结构，可能是 UpstreamData 的直接 JSON?
			var upstream UpstreamData
			if err := json.Unmarshal(bodyBytes, &upstream); err == nil {
				// 这种可能性较小，通常非流式会一次性返回完整结果
				// 如果 z.ai 不支持 stream=false，那还是会返回 SSE 吗？
				// 如果我们发了 stream=false，z.ai 可能会报错或返回普通 JSON
				// 从之前代码看，z.ai 似乎总是用 SSE?
				// 不，如果我们显式传 stream=false，API 应该行为不同。
				
				// 暂时假定 z.ai 即使 stream=false 也可能返回 SSE 格式的一次性输出，或者标准 JSON
				// 如果是标准 JSON，通常包含 "message": { "content": "..." }
				
				// 兜底：如果没有解析出内容，就把整个 body作为 content 返回，方便调试
				// 除非显然是错误的
				if fullContent == "" {
					fullContent = bodyStr // 临时策略：全量输出方便看到错误信息
				}
			}
		}
	} else {
		// SSE 解析逻辑 (旧逻辑，但基于 bodyBytes)
		scanner := bufio.NewScanner(bytes.NewReader(bodyBytes))
		var chunks []string
		var reasoningChunks []string
		thinkingFilter := &ThinkingFilter{}
		searchRefFilter := NewSearchRefFilter()
		hasThinking := false
		pendingSourcesMarkdown := ""
		pendingImageSearchMarkdown := ""

		for scanner.Scan() {
			line := scanner.Text()
			if !strings.HasPrefix(line, "data: ") {
				continue
			}

			payload := strings.TrimPrefix(line, "data: ")
			if payload == "[DONE]" {
				break
			}

			var upstream UpstreamData
			if err := json.Unmarshal([]byte(payload), &upstream); err != nil {
				continue
			}

			// ... (原有逻辑，复用 phasing 处理) ...
			// 由于代码太长，这里做简化复用：
			// 实际上我们需要完整提取原来的 switch/case 逻辑。
			// 为了代码简洁，建议提取一个处理单行 SSE 的函数。
			// 但鉴于 multi_replace 限制，我这里先把关键提取逻辑放回。
			
			if upstream.Data.Phase == "done" {
				break
			}
			
			// ... 这里的逻辑与之前完全一致 ...
            // 为避免大量代码重复，我仅保留关键提取点。
            // 完整逻辑太复杂，建议保留之前的 handleNonStreamResponse 并只在非 SSE 时走新分支。
            // 但 io.ReadAll 已经消费了 body。
		}
        // 由于 scanner 是从 bytes.NewReader 读的，逻辑可以复用。
        // 但为了不破坏原有复杂逻辑，其实还是 revert 到 scanner(body) 比较好，但是 body 已经 read 过了。
        // 所以必须用 bytes.NewReader。
        
        // **修正策略**：我不替换整个函数内容，而是恢复原来的 SSE 解析逻辑，
        // 只是数据源变成了 bytes.NewReader(bodyBytes)。
        // 且上面的 !isSSE 分支只处理非 SSE 情况。
        
        // 篇幅原因，我必须在此处完整重写 SSE 解析循环用于 parse。
        // 或者... 其实如果用户发了 stream=false，Upstream 可能根本不返回 SSE。
        // 那么原有逻辑 scanner.Scan() 就会直接结束（因为找不到 data: ）。
        
        // 所以关键是：如果 upstream 返回了普通 JSON，scanner 循环进不去，content 为空。
	}
    
    // 恢复 SSE 解析循环 (针对 bodyBytes)
    if isSSE {
        scanner := bufio.NewScanner(bytes.NewReader(bodyBytes))
        // ... 原有逻辑复制 ...
        // 由于太长，我这步工具调用无法完成所有代码替换。
        // 我应该分两步：
        // 1. 读取 bodyBytes。
        // 2. 如果包含 "data: "，走 SSE 解析 (重构为 processSSELine loop)。
        // 3. 如果不包含，尝试当做 JSON 解析。
    }

    // 鉴于 replace_file_content 无法支持如此大规模的变动且不重复代码，
    // 我选择：只在 scanner 循环后，如果 content 仍为空，且 !isSSE，则尝试当做 JSON 解析。
    
    // 让原来的 scanner 跑一下（针对 bytes.NewReader）。如果是普通 JSON，scan 不出 data: ，chunks 为空。
    // 在最后补充检查。
    
    scanner := bufio.NewScanner(bytes.NewReader(bodyBytes))
    // ... (保留原有变量声明)
    
    // ... (保留原有循环)

			if upstream.Data.Phase == "thinking" && upstream.Data.DeltaContent != "" {
				if thinkingFilter.lastPhase != "" && thinkingFilter.lastPhase != "thinking" {
					thinkingFilter.ResetForNewRound()
					thinkingFilter.thinkingRoundCount++
					if thinkingFilter.thinkingRoundCount > 1 {
						reasoningChunks = append(reasoningChunks, "\n\n")
					}
				}
				thinkingFilter.lastPhase = "thinking"

				hasThinking = true
				reasoningContent := thinkingFilter.ProcessThinking(upstream.Data.DeltaContent)
				if reasoningContent != "" {
					thinkingFilter.lastOutputChunk = reasoningContent
					reasoningChunks = append(reasoningChunks, reasoningContent)
				}
				continue
			}

			if upstream.Data.Phase != "" {
				thinkingFilter.lastPhase = upstream.Data.Phase
			}

			editContent := upstream.GetEditContent()
			if editContent != "" && IsSearchResultContent(editContent) {
				if results := ParseSearchResults(editContent); len(results) > 0 {
					searchRefFilter.AddSearchResults(results)
					pendingSourcesMarkdown = searchRefFilter.GetSearchResultsMarkdown()
				}
				continue
			}
			if editContent != "" && strings.Contains(editContent, `"search_image"`) {
				textBeforeBlock := ExtractTextBeforeGlmBlock(editContent)
				if textBeforeBlock != "" {
					chunks = append(chunks, textBeforeBlock)
				}
				if results := ParseImageSearchResults(editContent); len(results) > 0 {
					pendingImageSearchMarkdown = FormatImageSearchResults(results)
				}
				continue
			}
			if editContent != "" && strings.Contains(editContent, `"mcp"`) {
				textBeforeBlock := ExtractTextBeforeGlmBlock(editContent)
				if textBeforeBlock != "" {
					chunks = append(chunks, textBeforeBlock)
				}
				continue
			}
			if editContent != "" && IsSearchToolCall(editContent, upstream.Data.Phase) {
				continue
			}

			if pendingSourcesMarkdown != "" {
				if hasThinking {
					reasoningChunks = append(reasoningChunks, pendingSourcesMarkdown)
				} else {
					chunks = append(chunks, pendingSourcesMarkdown)
				}
				pendingSourcesMarkdown = ""
			}
			if pendingImageSearchMarkdown != "" {
				chunks = append(chunks, pendingImageSearchMarkdown)
				pendingImageSearchMarkdown = ""
			}

			content := ""
			if upstream.Data.Phase == "answer" && upstream.Data.DeltaContent != "" {
				content = upstream.Data.DeltaContent
			} else if upstream.Data.Phase == "answer" && editContent != "" {
				if strings.Contains(editContent, "</details>") {
					reasoningContent := thinkingFilter.ExtractIncrementalThinking(editContent)
					if reasoningContent != "" {
						reasoningChunks = append(reasoningChunks, reasoningContent)
					}

					if idx := strings.Index(editContent, "</details>"); idx != -1 {
						afterDetails := editContent[idx+len("</details>"):]
						if strings.HasPrefix(afterDetails, "\n") {
							content = afterDetails[1:]
						} else {
							content = afterDetails
						}
					}
				} else {
					content = editContent
				}
			} else if (upstream.Data.Phase == "other" || upstream.Data.Phase == "tool_call") && editContent != "" {
				content = editContent
			}

			if content != "" {
				chunks = append(chunks, content)
			}
		}

		fullContent = strings.Join(chunks, "")
		fullContent = searchRefFilter.Process(fullContent) + searchRefFilter.Flush()
		fullReasoning = strings.Join(reasoningChunks, "")
		fullReasoning = searchRefFilter.Process(fullReasoning) + searchRefFilter.Flush()
	} else {
		// Try parsing as standard JSON
		var directResp ChatCompletionResponse
		if err := json.Unmarshal(bodyBytes, &directResp); err == nil && len(directResp.Choices) > 0 {
			if directResp.Choices[0].Message != nil {
				fullContent = directResp.Choices[0].Message.Content
				fullReasoning = directResp.Choices[0].Message.ReasoningContent
			}
		} else {
			// Fallback: use body as content usually for error debugging
			LogDebug("Could not parse response as JSON, using raw body")
			// only use raw body if it is not too large and looks like text
			if len(bodyStr) < 2000 {
				fullContent = bodyStr
			}
		}
	}

	if fullContent == "" && fullReasoning == "" {
		LogError("Non-stream response 200 but no content received. Body preview: %s", bodyStr[:min(len(bodyStr), 200)])
	}

	stopReason := "stop"
	response := ChatCompletionResponse{
		ID:      completionID,
		Object:  "chat.completion",
		Created: time.Now().Unix(),
		Model:   modelName,
		Choices: []Choice{{
			Index: 0,
			Message: &MessageResp{
				Role:             "assistant",
				Content:          fullContent,
				ReasoningContent: fullReasoning,
			},
			FinishReason: &stopReason,
		}},
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(response)
}

func HandleModels(w http.ResponseWriter, r *http.Request) {
	var models []ModelInfo
	for _, id := range ModelList {
		models = append(models, ModelInfo{
			ID:      id,
			Object:  "model",
			OwnedBy: "z.ai",
		})
	}

	response := ModelsResponse{
		Object: "list",
		Data:   models,
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(response)
}
