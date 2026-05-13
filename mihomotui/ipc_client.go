package mihomotui

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"sync"
	"time"
)

// IPCClient IPC 客户端，通过 Unix Domain Socket 与服务端通讯
type IPCClient struct {
	client  *http.Client
	baseURL string
}

// NewIPCClient 创建 IPC 客户端
func NewIPCClient() (*IPCClient, error) {
	sock := socketPath()
	client := &http.Client{
		Timeout: DefaultIPCRequestTimeout,
		Transport: &http.Transport{
			DialContext: func(_ context.Context, _, _ string) (net.Conn, error) {
				return net.Dial("unix", sock)
			},
		},
	}
	return &IPCClient{
		client:  client,
		baseURL: "http://daemon",
	}, nil
}

// do 执行 HTTP 请求
func (c *IPCClient) do(method, path string, body []byte, query map[string]string) (*http.Response, error) {
	url := c.baseURL + path
	var bodyReader io.Reader
	if body != nil {
		bodyReader = bytes.NewReader(body)
	}
	req, err := http.NewRequest(method, url, bodyReader)
	if err != nil {
		return nil, err
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
	return c.client.Do(req)
}

// request 执行请求并读取响应
func (c *IPCClient) request(method, path string, body []byte, query map[string]string) ([]byte, error) {
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
		// 尝试解析服务端返回的 JSON 错误信息
		var apiErr APIResponse
		if err := json.Unmarshal(data, &apiErr); err == nil && apiErr.Error != "" {
			return nil, fmt.Errorf("%s", apiErr.Error)
		}
		return nil, fmt.Errorf("HTTP %d: %s", resp.StatusCode, string(data))
	}
	return data, nil
}

// requestJSON 执行请求并解析 JSON 响应
func (c *IPCClient) requestJSON(method, path string, body []byte, query map[string]string) (*APIResponse, error) {
	data, err := c.request(method, path, body, query)
	if err != nil {
		return nil, err
	}
	var resp APIResponse
	if err := json.Unmarshal(data, &resp); err != nil {
		return nil, fmt.Errorf("解析响应失败: %w", err)
	}
	if !resp.Success {
		if resp.Error != "" {
			return nil, fmt.Errorf("服务端错误: %s", resp.Error)
		}
		return nil, fmt.Errorf("服务端返回失败")
	}
	return &resp, nil
}

// streamRequest 执行流式请求（不关闭响应 Body，由调用方关闭）
func (c *IPCClient) streamRequest(method, path string, query map[string]string) (*http.Response, error) {
	client := &http.Client{
		Transport: &http.Transport{
			DialContext: func(_ context.Context, _, _ string) (net.Conn, error) {
				return net.Dial("unix", socketPath())
			},
		},
	}
	url := c.baseURL + path
	req, err := http.NewRequest(method, url, nil)
	if err != nil {
		return nil, err
	}
	if len(query) > 0 {
		q := req.URL.Query()
		for k, v := range query {
			q.Set(k, v)
		}
		req.URL.RawQuery = q.Encode()
	}
	return client.Do(req)
}

// unmarshalData 将 APIResponse.Data 反序列化为指定类型
func unmarshalData[T any](resp *APIResponse) (T, error) {
	var result T
	data, err := json.Marshal(resp.Data)
	if err != nil {
		return result, fmt.Errorf("序列化响应数据失败: %w", err)
	}
	if err := json.Unmarshal(data, &result); err != nil {
		return result, fmt.Errorf("解析响应数据失败: %w", err)
	}
	return result, nil
}

// ========== 单例管理 ==========

var (
	ipcClientSingleton *IPCClient
	ipcClientMu        sync.Mutex
)

// GetIPCClient 获取 IPC 客户端单例（线程安全）
func GetIPCClient() (*IPCClient, error) {
	ipcClientMu.Lock()
	defer ipcClientMu.Unlock()
	if ipcClientSingleton != nil {
		return ipcClientSingleton, nil
	}
	client, err := NewIPCClient()
	if err != nil {
		return nil, err
	}
	ipcClientSingleton = client
	return client, nil
}

// ResetIPCClient 重置 IPC 客户端（用于重新连接）
func ResetIPCClient() {
	ipcClientMu.Lock()
	defer ipcClientMu.Unlock()
	ipcClientSingleton = nil
}

// ========== 配置 ==========

// IPCGetConfig 从服务端获取配置
func (c *IPCClient) IPCGetConfig() (*Config, error) {
	resp, err := c.requestJSON(http.MethodGet, "/api/v1/config", nil, nil)
	if err != nil {
		return nil, err
	}
	cfgResp, err := unmarshalData[ConfigResponse](resp)
	if err != nil {
		return nil, fmt.Errorf("解析配置响应失败: %w", err)
	}
	return &cfgResp.Config, nil
}

// IPCUpdateConfig 更新服务端配置
func (c *IPCClient) IPCUpdateConfig(cfg *Config) error {
	body, err := json.Marshal(cfg)
	if err != nil {
		return err
	}
	_, err = c.requestJSON(http.MethodPost, "/api/v1/config", body, nil)
	return err
}

// ========== 订阅 ==========

// IPCGetSubscriptions 获取订阅列表
func (c *IPCClient) IPCGetSubscriptions() ([]SubscriptionMeta, error) {
	resp, err := c.requestJSON(http.MethodGet, "/api/v1/subscriptions", nil, nil)
	if err != nil {
		return nil, err
	}
	return unmarshalData[[]SubscriptionMeta](resp)
}

// IPCImportSubscription 导入订阅
func (c *IPCClient) IPCImportSubscription(url string) error {
	req := SubscriptionImportRequest{URL: url}
	body, err := json.Marshal(req)
	if err != nil {
		return err
	}
	_, err = c.requestJSON(http.MethodPost, "/api/v1/subscriptions", body, nil)
	return err
}

// IPCRefreshSubscription 刷新订阅
func (c *IPCClient) IPCRefreshSubscription(name string) error {
	_, err := c.requestJSON(http.MethodPut, "/api/v1/subscriptions/"+name, nil, nil)
	return err
}

// IPCDeleteSubscription 删除订阅
func (c *IPCClient) IPCDeleteSubscription(name string) error {
	_, err := c.requestJSON(http.MethodDelete, "/api/v1/subscriptions/"+name, nil, nil)
	return err
}

// IPCApplySubscription 应用订阅（生成 mihomo 配置）
func (c *IPCClient) IPCApplySubscription(name string) error {
	_, err := c.requestJSON(http.MethodPost, "/api/v1/subscriptions/"+name+"/apply", nil, nil)
	return err
}

// ========== mihomo 管理 ==========

// IPCGetMihomoStatus 获取 mihomo 进程状态
func (c *IPCClient) IPCGetMihomoStatus() (*MihomoStatusResponse, error) {
	resp, err := c.requestJSON(http.MethodGet, "/api/v1/mihomo/status", nil, nil)
	if err != nil {
		return nil, err
	}
	result, err := unmarshalData[MihomoStatusResponse](resp)
	if err != nil {
		return nil, fmt.Errorf("解析状态响应失败: %w", err)
	}
	return &result, nil
}

// IPCStartMihomo 启动 mihomo
func (c *IPCClient) IPCStartMihomo() error {
	_, err := c.requestJSON(http.MethodPost, "/api/v1/mihomo/start", nil, nil)
	return err
}

// IPCStopMihomo 停止 mihomo
func (c *IPCClient) IPCStopMihomo() error {
	_, err := c.requestJSON(http.MethodPost, "/api/v1/mihomo/stop", nil, nil)
	return err
}

// IPCRestartMihomo 重启 mihomo
func (c *IPCClient) IPCRestartMihomo() error {
	_, err := c.requestJSON(http.MethodPost, "/api/v1/mihomo/restart", nil, nil)
	return err
}

// IPCUpgradeMihomo 更新 mihomo（传入空字符串则自动获取最新版本）
func (c *IPCClient) IPCUpgradeMihomo(version string) error {
	body, _ := json.Marshal(map[string]string{"version": version})
	_, err := c.requestJSON(http.MethodPost, "/api/v1/mihomo/upgrade", body, nil)
	return err
}

// IPCGetMihomoUpgradeProgress 获取当前升级进度
func (c *IPCClient) IPCGetMihomoUpgradeProgress() (*UpgradeProgress, error) {
	resp, err := c.requestJSON(http.MethodGet, "/api/v1/mihomo/upgrade/progress", nil, nil)
	if err != nil {
		return nil, err
	}
	result, err := unmarshalData[UpgradeProgress](resp)
	if err != nil {
		return nil, fmt.Errorf("解析升级进度失败: %w", err)
	}
	return &result, nil
}

// IPCGetMihomoVersion 获取当前安装的 mihomo 版本
func (c *IPCClient) IPCGetMihomoVersion() (string, error) {
	resp, err := c.requestJSON(http.MethodGet, "/api/v1/mihomo/version", nil, nil)
	if err != nil {
		return "", err
	}
	result, err := unmarshalData[map[string]string](resp)
	if err != nil {
		return "", fmt.Errorf("解析版本响应失败: %w", err)
	}
	return result["version"], nil
}

// IPCGetMihomoLatestVersion 从 GitHub 获取最新 mihomo 版本
func (c *IPCClient) IPCGetMihomoLatestVersion() (string, error) {
	resp, err := c.requestJSON(http.MethodGet, "/api/v1/mihomo/latest-version", nil, nil)
	if err != nil {
		return "", err
	}
	result, err := unmarshalData[map[string]string](resp)
	if err != nil {
		return "", fmt.Errorf("解析版本响应失败: %w", err)
	}
	return result["version"], nil
}

// IPCCheckExternalResources 检查外部资源文件状态
func (c *IPCClient) IPCCheckExternalResources() ([]ExternalResourceInfo, error) {
	resp, err := c.requestJSON(http.MethodGet, "/api/v1/mihomo/external-resources", nil, nil)
	if err != nil {
		return nil, err
	}
	return unmarshalData[[]ExternalResourceInfo](resp)
}

// IPCDownloadExternalResources 下载外部资源文件
func (c *IPCClient) IPCDownloadExternalResources() error {
	_, err := c.requestJSON(http.MethodPost, "/api/v1/mihomo/external-resources/download", nil, nil)
	return err
}

// IPCShutdownDaemon 请求守护进程停止自身
func (c *IPCClient) IPCShutdownDaemon() error {
	_, err := c.requestJSON(http.MethodPost, "/api/v1/daemon/shutdown", nil, nil)
	return err
}

// IPCGetDaemonInfo 获取守护进程信息（启动模式、权限等）
func (c *IPCClient) IPCGetDaemonInfo() (*DaemonInfo, error) {
	resp, err := c.requestJSON(http.MethodGet, "/api/v1/daemon/info", nil, nil)
	if err != nil {
		return nil, err
	}
	result, err := unmarshalData[DaemonInfo](resp)
	if err != nil {
		return nil, fmt.Errorf("解析守护进程信息失败: %w", err)
	}
	return &result, nil
}

// IPCGetConfigDir 获取守护进程使用的配置目录
func (c *IPCClient) IPCGetConfigDir() (string, error) {
	resp, err := c.requestJSON(http.MethodGet, "/api/v1/daemon/config-dir", nil, nil)
	if err != nil {
		return "", err
	}
	result, err := unmarshalData[map[string]string](resp)
	if err != nil {
		return "", fmt.Errorf("解析配置目录响应失败: %w", err)
	}
	return result["config_dir"], nil
}

// IPCCheckDaemon 检查守护进程是否运行
func IPCCheckDaemon() bool {
	client, err := NewIPCClient()
	if err != nil {
		return false
	}
	resp, err := client.do(http.MethodGet, "/api/v1/ping", nil, nil)
	if err != nil {
		return false
	}
	defer resp.Body.Close()
	return resp.StatusCode == http.StatusOK
}

// IPCWaitForDaemon 等待守护进程就绪，超时返回错误
func IPCWaitForDaemon(timeout time.Duration) error {
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if IPCCheckDaemon() {
			return nil
		}
		time.Sleep(DefaultStreamInterval)
	}
	return fmt.Errorf("等待守护进程超时（%v）", timeout)
}

// ========== 规则订阅 ==========

// IPCGetRuleProviders 获取规则订阅列表
func (c *IPCClient) IPCGetRuleProviders() ([]RuleProviderSubscription, error) {
	resp, err := c.requestJSON(http.MethodGet, "/api/v1/rule-providers", nil, nil)
	if err != nil {
		return nil, err
	}
	return unmarshalData[[]RuleProviderSubscription](resp)
}

// IPCImportRuleProvider 导入规则订阅
func (c *IPCClient) IPCImportRuleProvider(req RuleProviderImportRequest) error {
	body, err := json.Marshal(req)
	if err != nil {
		return err
	}
	_, err = c.requestJSON(http.MethodPost, "/api/v1/rule-providers", body, nil)
	return err
}

// IPCRefreshRuleProvider 刷新规则订阅
func (c *IPCClient) IPCRefreshRuleProvider(name string) error {
	_, err := c.requestJSON(http.MethodPut, "/api/v1/rule-providers/"+name, nil, nil)
	return err
}

// IPCDeleteRuleProvider 删除规则订阅
func (c *IPCClient) IPCDeleteRuleProvider(name string) error {
	_, err := c.requestJSON(http.MethodDelete, "/api/v1/rule-providers/"+name, nil, nil)
	return err
}
