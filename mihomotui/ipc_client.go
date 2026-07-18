package mihomotui

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"reflect"
	"sync"
	"time"
)

// IPCClient IPC 客户端，通过 Unix Domain Socket 与服务端通讯
type IPCClient struct {
	client     *http.Client
	baseURL    string
	socketPath string
}

// NewIPCClient 创建 IPC 客户端
func NewIPCClient() (*IPCClient, error) {
	sock, err := clientSocketPathWithError()
	if err != nil {
		return nil, err
	}
	client := &http.Client{
		Timeout: DefaultIPCRequestTimeout,
		Transport: &http.Transport{
			DialContext: func(_ context.Context, _, _ string) (net.Conn, error) {
				return net.Dial("unix", sock)
			},
		},
	}
	return &IPCClient{
		client:     client,
		baseURL:    "http://daemon",
		socketPath: sock,
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
			switch resp.StatusCode {
			case http.StatusForbidden:
				return nil, fmt.Errorf("%w：%s", ErrIPCPermissionDenied, apiErr.Error)
			case http.StatusConflict:
				return nil, fmt.Errorf("%w：%s", ErrConfigConflict, apiErr.Error)
			}
			return nil, fmt.Errorf("%s", apiErr.Error)
		}
		if resp.StatusCode == http.StatusForbidden {
			return nil, fmt.Errorf("%w: HTTP %d: %s", ErrIPCPermissionDenied, resp.StatusCode, string(data))
		}
		if resp.StatusCode == http.StatusConflict {
			return nil, fmt.Errorf("%w: HTTP %d: %s", ErrConfigConflict, resp.StatusCode, string(data))
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
				return net.Dial("unix", c.socketPath)
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
	credentials, err := c.IPCGetMihomoAPICredentials()
	if err != nil {
		return nil, fmt.Errorf("获取 mihomo API 凭据失败: %w", err)
	}
	if controller := credentials["external_controller"]; controller != "" {
		cfgResp.Config.Mihomo.ExternalController = controller
	}
	cfgResp.Config.Mihomo.Secret = credentials["secret"]
	return &cfgResp.Config, nil
}

// IPCGetMihomoAPICredentials 获取受 IPC 授权保护的 mihomo API 最小连接凭据。
func (c *IPCClient) IPCGetMihomoAPICredentials() (map[string]string, error) {
	resp, err := c.requestJSON(http.MethodGet, "/api/v1/mihomo/api-credentials", nil, nil)
	if err != nil {
		return nil, err
	}
	return unmarshalData[map[string]string](resp)
}

// IPCUpdateConfig 更新服务端配置。
// 返回服务端提交后的配置快照与运行时应用结果；
// 版本冲突时返回以 ErrConfigConflict 包装的错误。
func (c *IPCClient) IPCUpdateConfig(cfg *Config) (*ConfigUpdateResponse, error) {
	body, err := json.Marshal(cfg)
	if err != nil {
		return nil, err
	}
	resp, err := c.requestJSON(http.MethodPost, "/api/v1/config", body, nil)
	if err != nil {
		return nil, err
	}
	result, err := unmarshalData[ConfigUpdateResponse](resp)
	if err != nil {
		return nil, fmt.Errorf("解析配置提交响应失败: %w", err)
	}
	return &result, nil
}

// SyncConfigFromServer 将服务端配置合并进本地缓存：
// 服务端是配置权威来源，但本地路径字段（mihomo 配置/二进制路径、日志目录）
// 属于各用户私有偏好，不随服务端覆盖；secret 为空（掩码响应）时保留本地值。
func SyncConfigFromServer(serverCfg *Config) {
	if serverCfg == nil {
		return
	}
	local := GlobalConfig()
	merged := serverCfg.Clone()
	merged.MihomoConfigPath = local.MihomoConfigPath
	merged.MihomoBinaryPath = local.MihomoBinaryPath
	merged.LogDir = local.LogDir
	if merged.Mihomo.Secret == "" {
		merged.Mihomo.Secret = local.Mihomo.Secret
	}
	SetGlobalConfig(merged)
}

// MutateServerConfig 以"读取最新 → 修改 → 提交"的方式更新服务端配置：
// 先从 daemon 获取最新配置（含当前版本号），应用 mutate 后提交；
// 若提交时版本冲突（配置已被其他会话修改），自动重新获取并重试一次。
// 成功后同步本地配置缓存（保留本地路径字段与 secret）。
// 返回服务端的提交响应（含运行时应用结果）。
func MutateServerConfig(mutate func(*Config)) (*ConfigUpdateResponse, error) {
	if mutate == nil {
		return nil, fmt.Errorf("配置修改函数不能为空")
	}
	client, err := GetIPCClient()
	if err != nil {
		return nil, err
	}
	var lastErr error
	for attempt := 0; attempt < 2; attempt++ {
		cfg, err := client.IPCGetConfig()
		if err != nil {
			return nil, err
		}
		before := cfg.Clone()
		mutate(cfg)
		// 两侧均取 Clone 再比较：Clone 会把空切片归一化为 nil，
		// 避免 JSON 反序列化产生的空非 nil 切片造成误判。
		if after := cfg.Clone(); reflect.DeepEqual(before, after) {
			// 修改未产生实际变化：跳过提交，避免无意义的版本递增、
			// 运行时应用与并发冲突（例如页面构建期控件回调触发的重复保存，
			// 或输入框失焦时值未改变的保存）。
			masked := *cfg
			masked.Mihomo.Secret = ""
			SyncConfigFromServer(&masked)
			Debugf("配置无实际变化，跳过提交")
			return &ConfigUpdateResponse{Config: masked, Applied: true}, nil
		}
		resp, err := client.IPCUpdateConfig(cfg)
		if err == nil {
			SyncConfigFromServer(&resp.Config)
			return resp, nil
		}
		if !errors.Is(err, ErrConfigConflict) {
			return nil, err
		}
		lastErr = err
		Infof("配置提交版本冲突，重新获取最新配置后重试（第 %d 次）", attempt+1)
	}
	return nil, lastErr
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

// IPCImportSubscription 导入远端订阅。
func (c *IPCClient) IPCImportSubscription(rawURL string) error {
	return c.IPCImportSubscriptionWithRequest(SubscriptionImportRequest{URL: rawURL})
}

// IPCImportSubscriptionWithRequest 导入远端订阅或创建手动订阅。
func (c *IPCClient) IPCImportSubscriptionWithRequest(req SubscriptionImportRequest) error {
	body, err := json.Marshal(req)
	if err != nil {
		return err
	}
	_, err = c.requestJSON(http.MethodPost, "/api/v1/subscriptions", body, nil)
	return err
}

// IPCRefreshSubscription 刷新订阅
func (c *IPCClient) IPCRefreshSubscription(name string) error {
	_, err := c.requestJSON(http.MethodPut, "/api/v1/subscriptions/"+url.PathEscape(name), nil, nil)
	return err
}

// IPCDeleteSubscription 删除订阅
func (c *IPCClient) IPCDeleteSubscription(name string) error {
	_, err := c.requestJSON(http.MethodDelete, "/api/v1/subscriptions/"+url.PathEscape(name), nil, nil)
	return err
}

// IPCApplySubscription 应用订阅（生成 mihomo 配置）
func (c *IPCClient) IPCApplySubscription(name string) error {
	_, err := c.requestJSON(http.MethodPost, "/api/v1/subscriptions/"+url.PathEscape(name)+"/apply", nil, nil)
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

// IPCProbeDaemon 检查守护进程并保留连接失败原因，避免把权限不足误报为服务未运行。
func IPCProbeDaemon() error {
	client, err := NewIPCClient()
	if err != nil {
		return err
	}
	resp, err := client.do(http.MethodGet, "/api/v1/ping", nil, nil)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("IPC 服务返回异常状态: %s", resp.Status)
	}
	return nil
}

// IPCCheckDaemon 检查守护进程是否运行
func IPCCheckDaemon() bool {
	return IPCProbeDaemon() == nil
}

// IPCWaitForDaemon 等待守护进程就绪，超时返回最后一次连接错误。
func IPCWaitForDaemon(timeout time.Duration) error {
	deadline := time.Now().Add(timeout)
	var lastErr error
	for time.Now().Before(deadline) {
		if err := IPCProbeDaemon(); err == nil {
			return nil
		} else {
			lastErr = err
		}
		time.Sleep(DefaultStreamInterval)
	}
	if lastErr != nil {
		return fmt.Errorf("等待守护进程超时（%v），最后错误: %w", timeout, lastErr)
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
	_, err := c.requestJSON(http.MethodPut, "/api/v1/rule-providers/"+url.PathEscape(name), nil, nil)
	return err
}

// IPCDeleteRuleProvider 删除规则订阅
func (c *IPCClient) IPCDeleteRuleProvider(name string) error {
	_, err := c.requestJSON(http.MethodDelete, "/api/v1/rule-providers/"+url.PathEscape(name), nil, nil)
	return err
}

// IPCGetSubscriptionPools 获取订阅池及其脱敏健康状态。
func (c *IPCClient) IPCGetSubscriptionPools() ([]SubscriptionPool, error) {
	resp, err := c.requestJSON(http.MethodGet, "/api/v1/subscription-pools", nil, nil)
	if err != nil {
		return nil, err
	}
	return unmarshalData[[]SubscriptionPool](resp)
}

// IPCImportSubscriptionContent 导入文件或粘贴正文；正文只在请求中传输。
func (c *IPCClient) IPCImportSubscriptionContent(name, source string, sourceType SubscriptionSource, content string, useProxy bool) error {
	return c.IPCImportSubscriptionWithRequest(SubscriptionImportRequest{Name: name, URL: source, SourceType: sourceType, Content: content, UseLocalProxy: useProxy})
}

// IPCCreateSubscriptionPool 创建顺序主备订阅池。
func (c *IPCClient) IPCCreateSubscriptionPool(req SubscriptionPoolRequest) (string, error) {
	body, err := json.Marshal(req)
	if err != nil {
		return "", err
	}
	resp, err := c.requestJSON(http.MethodPost, "/api/v1/subscription-pools", body, nil)
	if err != nil {
		return "", err
	}
	result, err := unmarshalData[map[string]string](resp)
	return result["id"], err
}

// IPCUpdateSubscriptionPool 更新成员顺序、活动源和刷新策略。
func (c *IPCClient) IPCUpdateSubscriptionPool(id string, req SubscriptionPoolRequest) error {
	body, err := json.Marshal(req)
	if err != nil {
		return err
	}
	_, err = c.requestJSON(http.MethodPut, "/api/v1/subscription-pools/"+url.PathEscape(id), body, nil)
	return err
}

// IPCDeleteSubscriptionPool 删除订阅池，不删除其中缓存的订阅源。
func (c *IPCClient) IPCDeleteSubscriptionPool(id string) error {
	_, err := c.requestJSON(http.MethodDelete, "/api/v1/subscription-pools/"+url.PathEscape(id), nil, nil)
	return err
}

// IPCRefreshSubscriptionPool 立即刷新订阅池全部远程成员。
func (c *IPCClient) IPCRefreshSubscriptionPool(id string) error {
	_, err := c.requestJSON(http.MethodPost, "/api/v1/subscription-pools/"+url.PathEscape(id)+"/refresh", nil, nil)
	return err
}
