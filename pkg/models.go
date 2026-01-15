package pkg

import (
	"encoding/json"
	"fmt"
	"regexp"
	"strings"
)

// 基础模型映射
var BaseModelMapping = map[string]string{
	"GLM-4.5":      "0727-360B-API",
	"GLM-4.6":      "GLM-4-6-API-V1",
	"GLM-4.7":      "glm-4.7",
	"GLM-4.5-V":    "glm-4.5v",
	"GLM-4.6-V":    "glm-4.6v",
	"GLM-4.5-Air":  "0727-106B-API",
	"0808-360B-DR": "0808-360B-DR",
}

// v1/models 杩斿洖鐨勬ā鍨嬪垪琛紙涓嶅寘鍚墍鏈夋爣绛剧粍鍚堬級
// v1/models 返回的模型列表
	"GLM-4.5",
	"GLM-4.6",
	"GLM-4.7",
	"GLM-4.7-thinking",
	"GLM-4.7-thinking-search",
	"GLM-4.5-V",
	"GLM-4.6-V",
	"GLM-4.6-V-thinking",
	"GLM-4.5-Air",
	// "0808-360B-DR",
}

// 瑙ｆ瀽妯″瀷鍚嶇О锛屾彁鍙栧熀纭€妯″瀷鍚嶅拰鏍囩
// 鏀寔 -thinking 鍜?-search 鏍囩鐨勪换鎰忔帓鍒楃粍鍚?func ParseModelName(model string) (baseModel string, enableThinking bool, enableSearch bool) {
	enableThinking = false
	enableSearch = false
	baseModel = model

	// 妫€鏌ュ苟绉婚櫎 -thinking 鍜?-search 鏍囩锛堜换鎰忛『搴忥級
	for {
		if strings.HasSuffix(baseModel, "-thinking") {
			enableThinking = true
			baseModel = strings.TrimSuffix(baseModel, "-thinking")
		} else if strings.HasSuffix(baseModel, "-search") {
			enableSearch = true
			baseModel = strings.TrimSuffix(baseModel, "-search")
		} else {
			break
		}
	}

	return baseModel, enableThinking, enableSearch
}

func IsThinkingModel(model string) bool {
	_, enableThinking, _ := ParseModelName(model)
	return enableThinking
}

func IsSearchModel(model string) bool {
	_, _, enableSearch := ParseModelName(model)
	return enableSearch
}

func GetTargetModel(model string) string {
	baseModel, _, _ := ParseModelName(model)
	if target, ok := BaseModelMapping[baseModel]; ok {
		return target
	}
	return model
}

// OpenAI 鏍煎紡鐨勬秷鎭唴瀹归」
type ContentPart struct {
	Type     string    `json:"type"`
	Text     string    `json:"text,omitempty"`
	ImageURL *ImageURL `json:"image_url,omitempty"`
}

type ImageURL struct {
	URL string `json:"url"`
}

// Message 鏀寔绾枃鏈拰澶氭ā鎬佸唴瀹?type Message struct {
	Role    string      `json:"role"`
	Content interface{} `json:"content"` // string 鎴?[]ContentPart
}

// 瑙ｆ瀽娑堟伅鍐呭锛岃繑鍥炴枃鏈拰鍥剧墖URL鍒楄〃
func (m *Message) ParseContent() (text string, imageURLs []string) {
	switch content := m.Content.(type) {
	case string:
		return content, nil
	case []interface{}:
		for _, item := range content {
			if part, ok := item.(map[string]interface{}); ok {
				partType, _ := part["type"].(string)
				if partType == "text" {
					if t, ok := part["text"].(string); ok {
						text += t
					}
				} else if partType == "image_url" {
					if imgURL, ok := part["image_url"].(map[string]interface{}); ok {
						if url, ok := imgURL["url"].(string); ok {
							imageURLs = append(imageURLs, url)
						}
					}
				}
			}
		}
	}
	return text, imageURLs
}

// 杞崲涓轰笂娓告秷鎭牸寮忥紝鏀寔澶氭ā鎬?func (m *Message) ToUpstreamMessage(urlToFileID map[string]string) map[string]interface{} {
	text, imageURLs := m.ParseContent()

	// 鏃犲浘鐗囷紝杩斿洖绾枃鏈?	if len(imageURLs) == 0 {
		return map[string]interface{}{
			"role":    m.Role,
			"content": text,
		}
	}

	// 鏈夊浘鐗囷紝鏋勫缓澶氭ā鎬佸唴瀹?	var content []interface{}
	if text != "" {
		content = append(content, map[string]interface{}{
			"type": "text",
			"text": text,
		})
	}
	for _, imgURL := range imageURLs {
		if fileID, ok := urlToFileID[imgURL]; ok {
			content = append(content, map[string]interface{}{
				"type": "image_url",
				"image_url": map[string]interface{}{
					"url": fileID,
				},
			})
		}
	}

	return map[string]interface{}{
		"role":    m.Role,
		"content": content,
	}
}

type ChatRequest struct {
	Model    string    `json:"model"`
	Messages []Message `json:"messages"`
	Stream   bool      `json:"stream"`
}

type ChatCompletionChunk struct {
	ID      string   `json:"id"`
	Object  string   `json:"object"`
	Created int64    `json:"created"`
	Model   string   `json:"model"`
	Choices []Choice `json:"choices"`
}

type Choice struct {
	Index        int          `json:"index"`
	Delta        Delta        `json:"delta,omitempty"`
	Message      *MessageResp `json:"message,omitempty"`
	FinishReason *string      `json:"finish_reason"`
}

type Delta struct {
	Content          string `json:"content,omitempty"`
	ReasoningContent string `json:"reasoning_content,omitempty"`
}

type MessageResp struct {
	Role             string `json:"role"`
	Content          string `json:"content"`
	ReasoningContent string `json:"reasoning_content,omitempty"`
}

type ChatCompletionResponse struct {
	ID      string   `json:"id"`
	Object  string   `json:"object"`
	Created int64    `json:"created"`
	Model   string   `json:"model"`
	Choices []Choice `json:"choices"`
}

type ModelsResponse struct {
	Object string      `json:"object"`
	Data   []ModelInfo `json:"data"`
}

type ModelInfo struct {
	ID      string `json:"id"`
	Object  string `json:"object"`
	OwnedBy string `json:"owned_by"`
}

var searchRefPattern = regexp.MustCompile(`銆恡urn\d+search(\d+)銆慲)
var searchRefPrefixPattern = regexp.MustCompile(`銆?t(u(r(n(\d+(s(e(a(r(c(h(\d+)?)?)?)?)?)?)?)?)?)?)?)?$`)

type SearchResult struct {
	Title string `json:"title"`
	URL   string `json:"url"`
	Index int    `json:"index"`
	RefID string `json:"ref_id"`
}

type SearchRefFilter struct {
	buffer        string
	searchResults map[string]SearchResult
}

func NewSearchRefFilter() *SearchRefFilter {
	return &SearchRefFilter{
		searchResults: make(map[string]SearchResult),
	}
}

func (f *SearchRefFilter) AddSearchResults(results []SearchResult) {
	for _, r := range results {
		f.searchResults[r.RefID] = r
	}
}

func escapeMarkdownTitle(title string) string {
	title = strings.ReplaceAll(title, `\`, `\\`)
	title = strings.ReplaceAll(title, `[`, `\[`)
	title = strings.ReplaceAll(title, `]`, `\]`)
	return title
}

func (f *SearchRefFilter) Process(content string) string {
	content = f.buffer + content
	f.buffer = ""

	if content == "" {
		return ""
	}

	content = searchRefPattern.ReplaceAllStringFunc(content, func(match string) string {
		runes := []rune(match)
		refID := string(runes[1 : len(runes)-1])
		if result, ok := f.searchResults[refID]; ok {
			return fmt.Sprintf(`[\[%d\]](%s)`, result.Index, result.URL)
		}
		return ""
	})

	if content == "" {
		return ""
	}

	maxPrefixLen := 20
	if len(content) < maxPrefixLen {
		maxPrefixLen = len(content)
	}

	for i := 1; i <= maxPrefixLen; i++ {
		suffix := content[len(content)-i:]
		if searchRefPrefixPattern.MatchString(suffix) {
			f.buffer = suffix
			return content[:len(content)-i]
		}
	}

	return content
}

func (f *SearchRefFilter) Flush() string {
	result := f.buffer
	f.buffer = ""
	if result != "" {
		result = searchRefPattern.ReplaceAllStringFunc(result, func(match string) string {
			runes := []rune(match)
			refID := string(runes[1 : len(runes)-1])
			if r, ok := f.searchResults[refID]; ok {
				return fmt.Sprintf(`[\[%d\]](%s)`, r.Index, r.URL)
			}
			return ""
		})
	}
	return result
}

func (f *SearchRefFilter) GetSearchResultsMarkdown() string {
	if len(f.searchResults) == 0 {
		return ""
	}

	var results []SearchResult
	for _, r := range f.searchResults {
		results = append(results, r)
	}
	for i := 0; i < len(results)-1; i++ {
		for j := i + 1; j < len(results); j++ {
			if results[i].Index > results[j].Index {
				results[i], results[j] = results[j], results[i]
			}
		}
	}

	var sb strings.Builder
	for _, r := range results {
		escapedTitle := escapeMarkdownTitle(r.Title)
		sb.WriteString(fmt.Sprintf("[\\[%d\\] %s](%s)\n", r.Index, escapedTitle, r.URL))
	}
	sb.WriteString("\n")
	return sb.String()
}

func IsSearchResultContent(editContent string) bool {
	return strings.Contains(editContent, `"search_result"`)
}

func ParseSearchResults(editContent string) []SearchResult {
	searchResultKey := `"search_result":`
	idx := strings.Index(editContent, searchResultKey)
	if idx == -1 {
		return nil
	}

	startIdx := idx + len(searchResultKey)
	for startIdx < len(editContent) && editContent[startIdx] != '[' {
		startIdx++
	}
	if startIdx >= len(editContent) {
		return nil
	}

	bracketCount := 0
	endIdx := startIdx
	for endIdx < len(editContent) {
		if editContent[endIdx] == '[' {
			bracketCount++
		} else if editContent[endIdx] == ']' {
			bracketCount--
			if bracketCount == 0 {
				endIdx++
				break
			}
		}
		endIdx++
	}

	if bracketCount != 0 {
		return nil
	}

	jsonStr := editContent[startIdx:endIdx]
	var rawResults []struct {
		Title string `json:"title"`
		URL   string `json:"url"`
		Index int    `json:"index"`
		RefID string `json:"ref_id"`
	}

	if err := json.Unmarshal([]byte(jsonStr), &rawResults); err != nil {
		return nil
	}

	var results []SearchResult
	for _, r := range rawResults {
		results = append(results, SearchResult{
			Title: r.Title,
			URL:   r.URL,
			Index: r.Index,
			RefID: r.RefID,
		})
	}

	return results
}

func IsSearchToolCall(editContent string, phase string) bool {
	if phase != "tool_call" {
		return false
	}
	// tool_call 闃舵鍖呭惈 mcp 鐩稿叧鍐呭鐨勯兘璺宠繃
	return strings.Contains(editContent, `"mcp"`) || strings.Contains(editContent, `mcp-server`)
}

type ImageSearchResult struct {
	Title     string `json:"title"`
	Link      string `json:"link"`
	Thumbnail string `json:"thumbnail"`
}

func ParseImageSearchResults(editContent string) []ImageSearchResult {
	resultKey := `"result":`
	idx := strings.Index(editContent, resultKey)
	if idx == -1 {
		return nil
	}

	startIdx := idx + len(resultKey)
	for startIdx < len(editContent) && editContent[startIdx] != '[' {
		startIdx++
	}
	if startIdx >= len(editContent) {
		return nil
	}

	bracketCount := 0
	endIdx := startIdx
	inString := false
	escapeNext := false
	for endIdx < len(editContent) {
		ch := editContent[endIdx]

		if escapeNext {
			escapeNext = false
			endIdx++
			continue
		}

		if ch == '\\' {
			escapeNext = true
			endIdx++
			continue
		}

		if ch == '"' {
			inString = !inString
		}

		if !inString {
			if ch == '[' || ch == '{' {
				bracketCount++
			} else if ch == ']' || ch == '}' {
				bracketCount--
				if bracketCount == 0 && ch == ']' {
					endIdx++
					break
				}
			}
		}
		endIdx++
	}

	if bracketCount != 0 {
		return nil
	}

	jsonStr := editContent[startIdx:endIdx]

	var rawResults []map[string]interface{}
	if err := json.Unmarshal([]byte(jsonStr), &rawResults); err != nil {
		return nil
	}

	var results []ImageSearchResult
	for _, item := range rawResults {
		if itemType, ok := item["type"].(string); ok && itemType == "text" {
			if text, ok := item["text"].(string); ok {
				result := parseImageSearchText(text)
				if result.Title != "" && result.Link != "" {
					results = append(results, result)
				}
			}
		}
	}

	return results
}

func parseImageSearchText(text string) ImageSearchResult {
	result := ImageSearchResult{}

	if titleIdx := strings.Index(text, "Title: "); titleIdx != -1 {
		titleStart := titleIdx + len("Title: ")
		titleEnd := strings.Index(text[titleStart:], ";")
		if titleEnd != -1 {
			result.Title = strings.TrimSpace(text[titleStart : titleStart+titleEnd])
		}
	}

	if linkIdx := strings.Index(text, "Link: "); linkIdx != -1 {
		linkStart := linkIdx + len("Link: ")
		linkEnd := strings.Index(text[linkStart:], ";")
		if linkEnd != -1 {
			result.Link = strings.TrimSpace(text[linkStart : linkStart+linkEnd])
		} else {
			result.Link = strings.TrimSpace(text[linkStart:])
		}
	}

	if thumbnailIdx := strings.Index(text, "Thumbnail: "); thumbnailIdx != -1 {
		thumbnailStart := thumbnailIdx + len("Thumbnail: ")
		result.Thumbnail = strings.TrimSpace(text[thumbnailStart:])
	}

	return result
}

func FormatImageSearchResults(results []ImageSearchResult) string {
	if len(results) == 0 {
		return ""
	}

	var sb strings.Builder
	for _, r := range results {
		escapedTitle := strings.ReplaceAll(r.Title, `[`, `\[`)
		escapedTitle = strings.ReplaceAll(escapedTitle, `]`, `\]`)
		sb.WriteString(fmt.Sprintf("\n![%s](%s)", escapedTitle, r.Link))
	}
	sb.WriteString("\n")
	return sb.String()
}

func ExtractTextBeforeGlmBlock(editContent string) string {
	if idx := strings.Index(editContent, "<glm_block"); idx != -1 {
		text := editContent[:idx]
		if strings.HasSuffix(text, "\n") {
			text = text[:len(text)-1]
		}
		return text
	}
	return ""
}
