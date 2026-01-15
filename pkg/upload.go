package pkg

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"path/filepath"
	"strings"

	"github.com/google/uuid"
)

// FileUploadResponse z.ai 文件上传响应
type FileUploadResponse struct {
	ID       string `json:"id"`
	UserID   string `json:"user_id"`
	Filename string `json:"filename"`
	Meta     struct {
		Name        string `json:"name"`
		ContentType string `json:"content_type"`
		Size        int64  `json:"size"`
		CdnURL      string `json:"cdn_url"`
	} `json:"meta"`
}

// UpstreamFile 上游请求的文件格�?type UpstreamFile struct {
	Type   string             `json:"type"`
	File   FileUploadResponse `json:"file"`
	ID     string             `json:"id"`
	URL    string             `json:"url"`
	Name   string             `json:"name"`
	Status string             `json:"status"`
	Size   int64              `json:"size"`
	Error  string             `json:"error"`
	ItemID string             `json:"itemId"`
	Media  string             `json:"media"`
}

// UploadImageFromURL �?URL �?base64 上传图片�?z.ai
func UploadImageFromURL(token string, imageURL string) (*UpstreamFile, error) {
	var imageData []byte
	var filename string
	var contentType string

	if strings.HasPrefix(imageURL, "data:") {
		// Base64 编码的图�?		// 格式: data:image/jpeg;base64,/9j/4AAQ...
		parts := strings.SplitN(imageURL, ",", 2)
		if len(parts) != 2 {
			return nil, fmt.Errorf("invalid base64 image format")
		}

		// 解析 MIME 类型
		header := parts[0] // data:image/jpeg;base64
		if idx := strings.Index(header, ":"); idx != -1 {
			mimeAndEncoding := header[idx+1:]
			if semiIdx := strings.Index(mimeAndEncoding, ";"); semiIdx != -1 {
				contentType = mimeAndEncoding[:semiIdx]
			}
		}
		if contentType == "" {
			contentType = "image/png"
		}

		// 解码 base64
		var err error
		imageData, err = base64.StdEncoding.DecodeString(parts[1])
		if err != nil {
			return nil, fmt.Errorf("failed to decode base64: %v", err)
		}

		// 生成文件�?		ext := ".png"
		if strings.Contains(contentType, "jpeg") || strings.Contains(contentType, "jpg") {
			ext = ".jpg"
		} else if strings.Contains(contentType, "gif") {
			ext = ".gif"
		} else if strings.Contains(contentType, "webp") {
			ext = ".webp"
		}
		filename = uuid.New().String()[:12] + ext
	} else {
		// �?URL 下载图片
		resp, err := http.Get(imageURL)
		if err != nil {
			return nil, fmt.Errorf("failed to download image: %v", err)
		}
		defer resp.Body.Close()

		if resp.StatusCode != http.StatusOK {
			return nil, fmt.Errorf("failed to download image: status %d", resp.StatusCode)
		}

		imageData, err = io.ReadAll(resp.Body)
		if err != nil {
			return nil, fmt.Errorf("failed to read image data: %v", err)
		}

		contentType = resp.Header.Get("Content-Type")
		if contentType == "" {
			contentType = "image/png"
		}

		// �?URL 提取文件�?		filename = filepath.Base(imageURL)
		if filename == "" || filename == "." || filename == "/" {
			ext := ".png"
			if strings.Contains(contentType, "jpeg") || strings.Contains(contentType, "jpg") {
				ext = ".jpg"
			}
			filename = uuid.New().String()[:12] + ext
		}
	}

	// 构建 multipart form 请求
	var buf bytes.Buffer
	writer := multipart.NewWriter(&buf)

	part, err := writer.CreateFormFile("file", filename)
	if err != nil {
		return nil, fmt.Errorf("failed to create form file: %v", err)
	}

	if _, err := part.Write(imageData); err != nil {
		return nil, fmt.Errorf("failed to write image data: %v", err)
	}

	writer.Close()

	// 发送上传请�?	req, err := http.NewRequest("POST", "https://chat.z.ai/api/v1/files/", &buf)
	if err != nil {
		return nil, fmt.Errorf("failed to create upload request: %v", err)
	}

	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Content-Type", writer.FormDataContentType())
	req.Header.Set("Origin", "https://chat.z.ai")
	req.Header.Set("Referer", "https://chat.z.ai/")

	client := &http.Client{}
	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("failed to upload image: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusCreated {
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("upload failed: status %d, body: %s", resp.StatusCode, string(body))
	}

	var uploadResp FileUploadResponse
	if err := json.NewDecoder(resp.Body).Decode(&uploadResp); err != nil {
		return nil, fmt.Errorf("failed to parse upload response: %v", err)
	}

	// 构建上游文件格式
	return &UpstreamFile{
		Type:   "image",
		File:   uploadResp,
		ID:     uploadResp.ID,
		URL:    fmt.Sprintf("/api/v1/files/%s/content", uploadResp.ID),
		Name:   uploadResp.Filename,
		Status: "uploaded",
		Size:   uploadResp.Meta.Size,
		Error:  "",
		ItemID: uuid.New().String(),
		Media:  "image",
	}, nil
}

// UploadImages 批量上传图片
func UploadImages(token string, imageURLs []string) ([]*UpstreamFile, error) {
	var files []*UpstreamFile
	for _, url := range imageURLs {
		file, err := UploadImageFromURL(token, url)
		if err != nil {
			LogError("Failed to upload image %s: %v", url[:min(50, len(url))], err)
			continue
		}
		files = append(files, file)
	}
	return files, nil
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}
