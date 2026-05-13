package mihomotui

import (
	"bytes"
	"fmt"
	"io"
	"net/http"
	"sync"
)

// MihomoAPI mihomo REST API 客户端
type MihomoAPI struct {
	client  *http.Client
	baseURL string
	secret  string
}

// NewMihomoAPI 创建 mihomo API 客户端
func NewMihomoAPI(baseURL, secret string) *MihomoAPI {
	return &MihomoAPI{
		client:  &http.Client{Timeout: DefaultIPCRequestTimeout},
		baseURL: baseURL,
		secret:  secret,
	}
}

// NewMihomoAPIFromConfig 从全局配置创建 API 客户端
func NewMihomoAPIFromConfig() *MihomoAPI {
	cfg := GlobalConfig()
	baseURL := "http://" + cfg.Mihomo.ExternalController
	return NewMihomoAPI(baseURL, cfg.Mihomo.Secret)
}

// buildRequest 构造 HTTP 请求
func (c *MihomoAPI) buildRequest(method, path string, body []byte, query map[string]string) (*http.Request, error) {
	url := c.baseURL + path
	var bodyReader io.Reader
	if body != nil {
		bodyReader = bytes.NewReader(body)
	}
	req, err := http.NewRequest(method, url, bodyReader)
	if err != nil {
		return nil, err
	}
	if c.secret != "" {
		req.Header.Set("Authorization", "Bearer "+c.secret)
	}
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	if len(query) > 0 {
		q := req.URL.Query()
		for k, v := range query {
			q.Set(k, v)
		}
		req.URL.RawQuery = q.Encode()
	}
	return req, nil
}

// do 执行普通 HTTP 请求（带超时）
func (c *MihomoAPI) do(method, path string, body []byte, query map[string]string) (*http.Response, error) {
	req, err := c.buildRequest(method, path, body, query)
	if err != nil {
		return nil, err
	}
	return c.client.Do(req)
}

// doStream 执行流式 HTTP 请求（无超时，用于日志/流量/内存/连接等长连接）
func (c *MihomoAPI) doStream(method, path string, query map[string]string) (*http.Response, error) {
	req, err := c.buildRequest(method, path, nil, query)
	if err != nil {
		return nil, err
	}
	client := &http.Client{}
	return client.Do(req)
}

// request 执行请求并读取完整响应 body
func (c *MihomoAPI) request(method, path string, body []byte, query map[string]string) ([]byte, error) {
	resp, err := c.do(method, path, body, query)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	data, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}
	if resp.StatusCode >= 400 {
		return nil, fmt.Errorf("HTTP %d: %s", resp.StatusCode, string(data))
	}
	return data, nil
}

// ========== 单例管理 ==========

var (
	mihomoAPISingleton *MihomoAPI
	mihomoAPIMu        sync.Mutex
)

// GetMihomoAPI 获取 mihomo API 客户端单例（线程安全）
func GetMihomoAPI() (*MihomoAPI, error) {
	mihomoAPIMu.Lock()
	defer mihomoAPIMu.Unlock()
	if mihomoAPISingleton != nil {
		return mihomoAPISingleton, nil
	}
	cfg := GlobalConfig()
	if cfg.Mihomo.ExternalController == "" {
		return nil, fmt.Errorf("mihomo external-controller 未配置")
	}
	mihomoAPISingleton = NewMihomoAPIFromConfig()
	return mihomoAPISingleton, nil
}

// ResetMihomoAPI 重置 mihomo API 客户端单例（线程安全）
func ResetMihomoAPI() {
	mihomoAPIMu.Lock()
	defer mihomoAPIMu.Unlock()
	mihomoAPISingleton = nil
}
