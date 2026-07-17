package mihomotui

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"
)

// TestReconcileSerializesApplyTasks 验证配置应用任务严格串行执行，
// 且每个等待中的调用方都能拿到自己任务的执行结果。
func TestReconcileSerializesApplyTasks(t *testing.T) {
	var mu sync.Mutex
	executing := 0
	maxConcurrent := 0
	var completed []string

	d := &Daemon{}
	d.reconcileApply = func(req reconcileRequest) ApplyReport {
		mu.Lock()
		executing++
		if executing > maxConcurrent {
			maxConcurrent = executing
		}
		mu.Unlock()
		time.Sleep(20 * time.Millisecond)
		mu.Lock()
		executing--
		completed = append(completed, req.reason)
		mu.Unlock()
		return ApplyReport{Applied: true}
	}

	const n = 5
	var wg sync.WaitGroup
	for i := 0; i < n; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			report := d.reconcileLatest(fmt.Sprintf("job-%d", i))
			if !report.Applied {
				t.Errorf("job-%d report = %+v, want applied", i, report)
			}
		}(i)
	}
	wg.Wait()

	if maxConcurrent != 1 {
		t.Fatalf("apply tasks overlapped: max concurrent = %d, want 1", maxConcurrent)
	}
	if len(completed) != n {
		t.Fatalf("completed jobs = %d, want %d", len(completed), n)
	}
}

// TestReconcileApplyFailurePropagates 验证应用失败结果带阶段信息返回给调用方，
// 且 TUN 同步意图由提交前后的 TUN 状态变化推出。
func TestReconcileApplyFailurePropagates(t *testing.T) {
	var gotReq reconcileRequest
	d := &Daemon{}
	d.reconcileApply = func(req reconcileRequest) ApplyReport {
		gotReq = req
		return ApplyReport{Applied: false, Stage: "reload", Err: "模拟热重载失败"}
	}
	report := d.reconcileConfigChange("config", false, true)
	if report.Applied || report.Stage != "reload" || !strings.Contains(report.Err, "模拟热重载失败") {
		t.Fatalf("report = %+v", report)
	}
	if !gotReq.syncTUN || gotReq.oldTUN || !gotReq.newTUN {
		t.Fatalf("TUN sync intent not derived correctly: %+v", gotReq)
	}
}

// postConfig 辅助：构造配置 POST 请求并调用 handler。
func postConfig(t *testing.T, d *Daemon, cfg Config) (*httptest.ResponseRecorder, ConfigUpdateResponse) {
	t.Helper()
	body, err := json.Marshal(cfg)
	if err != nil {
		t.Fatal(err)
	}
	recorder := httptest.NewRecorder()
	d.handleConfig(recorder, httptest.NewRequest(http.MethodPost, "/api/v1/config", bytes.NewReader(body)))
	return recorder, ConfigUpdateResponse{}
}

func parseConfigUpdateResponse(t *testing.T, recorder *httptest.ResponseRecorder) ConfigUpdateResponse {
	t.Helper()
	var resp APIResponse
	if err := json.Unmarshal(recorder.Body.Bytes(), &resp); err != nil {
		t.Fatal(err)
	}
	result, err := unmarshalData[ConfigUpdateResponse](&resp)
	if err != nil {
		t.Fatal(err)
	}
	return result
}

// TestHandleConfigPostValidationConflictAndApplyStatus 覆盖 P1-3/P1-5 验收标准：
// 非法配置 400；陈旧版本 409；保存成功但应用失败时调用方能拿到阶段与原因。
func TestHandleConfigPostValidationConflictAndApplyStatus(t *testing.T) {
	useTestConfigDir(t)

	applyErr := ""
	d := &Daemon{}
	d.reconcileApply = func(req reconcileRequest) ApplyReport {
		if applyErr != "" {
			return ApplyReport{Applied: false, Stage: "reload", Err: applyErr}
		}
		return ApplyReport{Applied: true}
	}

	// 1. 非法配置（端口冲突）→ 400，状态未变
	bad := defaultConfig()
	bad.Mihomo.HTTPPort = 8080
	bad.Mihomo.MixedPort = 8080
	recorder, _ := postConfig(t, d, bad)
	if recorder.Code != http.StatusBadRequest {
		t.Fatalf("invalid config status = %d, want 400", recorder.Code)
	}
	if got := GlobalConfig().Mihomo.HTTPPort; got != 7890 {
		t.Fatalf("invalid commit changed state: http port = %d", got)
	}

	// 2. 基于当前版本（0）的合法提交 → 200 + applied
	good := *GlobalConfig()
	good.ProxyMode = "global"
	recorder, _ = postConfig(t, d, good)
	if recorder.Code != http.StatusOK {
		t.Fatalf("valid config status = %d, want 200: %s", recorder.Code, recorder.Body.String())
	}
	result := parseConfigUpdateResponse(t, recorder)
	if !result.Applied || result.Config.Version != 1 || result.Config.ProxyMode != "global" {
		t.Fatalf("unexpected update response: %+v", result)
	}
	if result.Config.Mihomo.Secret != "" {
		t.Fatal("update response leaked secret")
	}

	// 3. 基于过期版本（0）再次提交 → 409
	stale := good // version 0
	stale.ProxyMode = "rule"
	recorder, _ = postConfig(t, d, stale)
	if recorder.Code != http.StatusConflict {
		t.Fatalf("stale config status = %d, want 409", recorder.Code)
	}
	if got := GlobalConfig().ProxyMode; got != "global" {
		t.Fatalf("conflicting commit changed state: %q", got)
	}

	// 4. 基于最新版本提交，但运行时应用失败 → 200 + applied=false + 阶段信息
	applyErr = "热重载失败: connection refused"
	fresh := *GlobalConfig() // version 1
	fresh.ProxyMode = "direct"
	recorder, _ = postConfig(t, d, fresh)
	if recorder.Code != http.StatusOK {
		t.Fatalf("apply-failure status = %d, want 200（配置已保存）", recorder.Code)
	}
	result = parseConfigUpdateResponse(t, recorder)
	if result.Applied || result.ApplyStage != "reload" || !strings.Contains(result.ApplyError, "热重载失败") {
		t.Fatalf("apply failure not reported: %+v", result)
	}
	if got := GlobalConfig().ProxyMode; got != "direct" {
		t.Fatalf("config was not committed despite apply failure: %q", got)
	}
}

// TestIPCConflictIsClassified 验证客户端将 HTTP 409 归类为 ErrConfigConflict。
func TestIPCConflictIsClassified(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		writeError(w, http.StatusConflict, fmt.Errorf("%w: 配置已被其他会话修改", ErrConfigConflict))
	}))
	defer server.Close()

	client := &IPCClient{client: server.Client(), baseURL: server.URL}
	_, err := client.request(http.MethodPost, "/api/v1/config", nil, nil)
	if !errors.Is(err, ErrConfigConflict) {
		t.Fatalf("HTTP 409 was not classified as config conflict: %v", err)
	}
}
