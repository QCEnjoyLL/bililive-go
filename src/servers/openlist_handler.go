package servers

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/bililive-go/bililive-go/src/configs"
	"github.com/bililive-go/bililive-go/src/pkg/openlist"
)

// OpenListStatusResponse OpenList 状态响应
type OpenListStatusResponse struct {
	OpenListRunning    bool                   `json:"openlist_running"`
	WebUIPath          string                 `json:"web_ui_path"`
	External           bool                   `json:"external"`
	APIEndpoint        string                 `json:"api_endpoint,omitempty"`
	Storages           []openlist.StorageInfo `json:"storages"`
	Errors             []string               `json:"errors"`
	CloudUploadEnabled bool                   `json:"cloud_upload_enabled"`
}

// OpenListStorageHealthResponse 存储健康检查响应
type OpenListStorageHealthResponse struct {
	Healthy bool   `json:"healthy"`
	Message string `json:"message,omitempty"`
}

// OpenListValidateRequest 用于临时验证 OpenList 连接配置，不要求先保存配置。
type OpenListValidateRequest struct {
	ExternalURL   string `json:"external_url"`
	ExternalToken string `json:"external_token"`
	StorageName   string `json:"storage_name"`
}

// OpenListValidateResponse OpenList 连接验证结果。
type OpenListValidateResponse struct {
	OK             bool                   `json:"ok"`
	ReadyForUpload bool                   `json:"ready_for_upload"`
	External       bool                   `json:"external"`
	Endpoint       string                 `json:"endpoint,omitempty"`
	ServiceReady   bool                   `json:"service_ready"`
	AuthChecked    bool                   `json:"auth_checked"`
	AuthOK         bool                   `json:"auth_ok"`
	StorageChecked bool                   `json:"storage_checked"`
	StorageOK      bool                   `json:"storage_ok"`
	Storages       []openlist.StorageInfo `json:"storages"`
	Errors         []string               `json:"errors"`
	Message        string                 `json:"message"`
}

// 全局 OpenList 管理器引用（由 main 设置）
var globalOpenListManager *openlist.Manager

// SetOpenListManager 设置全局 OpenList 管理器
func SetOpenListManager(m *openlist.Manager) {
	globalOpenListManager = m
}

// getOpenListStatus 获取 OpenList 状态
func getOpenListStatus(writer http.ResponseWriter, r *http.Request) {
	config := configs.GetCurrentConfig()

	response := OpenListStatusResponse{
		CloudUploadEnabled: config.OnRecordFinished.CloudUpload.Enable,
		WebUIPath:          "/remotetools/tool/openlist/",
		Storages:           []openlist.StorageInfo{},
		Errors:             []string{},
	}

	// 检查 OpenList 管理器是否存在
	if globalOpenListManager == nil {
		if config.OnRecordFinished.CloudUpload.Enable {
			response.Errors = append(response.Errors, "OpenList 管理器未初始化")
		}
		writer.Header().Set("Content-Type", "application/json")
		json.NewEncoder(writer).Encode(response)
		return
	}

	// 检查 OpenList 是否运行
	response.OpenListRunning = globalOpenListManager.IsRunning()
	response.WebUIPath = globalOpenListManager.GetWebUIPath()
	response.External = globalOpenListManager.IsExternal()
	response.APIEndpoint = globalOpenListManager.GetAPIEndpoint()

	if !response.OpenListRunning {
		response.Errors = append(response.Errors, "OpenList 服务未运行")
		writer.Header().Set("Content-Type", "application/json")
		json.NewEncoder(writer).Encode(response)
		return
	}

	// 尝试获取存储列表
	client := openlist.NewClient(globalOpenListManager.GetAPIEndpoint(), globalOpenListManager.GetAPIToken())
	ctx, cancel := context.WithTimeout(r.Context(), 5*time.Second)
	defer cancel()

	storages, err := client.ListStorages(ctx)
	if err != nil {
		response.Errors = append(response.Errors, "无法获取存储列表: "+err.Error())
	} else {
		response.Storages = storages
		if len(storages) == 0 {
			response.Errors = append(response.Errors, "未配置任何存储，请在 OpenList 中添加网盘")
		}
	}

	writer.Header().Set("Content-Type", "application/json")
	json.NewEncoder(writer).Encode(response)
}

// checkOpenListStorageHealth 检查存储健康状态
func checkOpenListStorageHealth(writer http.ResponseWriter, r *http.Request) {
	storageName := r.URL.Query().Get("name")
	if storageName == "" {
		http.Error(writer, "缺少 name 参数", http.StatusBadRequest)
		return
	}

	response := OpenListStorageHealthResponse{
		Healthy: false,
	}

	if globalOpenListManager == nil || !globalOpenListManager.IsRunning() {
		response.Message = "OpenList 服务未运行"
		writer.Header().Set("Content-Type", "application/json")
		json.NewEncoder(writer).Encode(response)
		return
	}

	client := openlist.NewClient(globalOpenListManager.GetAPIEndpoint(), globalOpenListManager.GetAPIToken())
	ctx, cancel := context.WithTimeout(r.Context(), 10*time.Second)
	defer cancel()

	if err := client.CheckStorageHealth(ctx, storageName); err != nil {
		response.Message = err.Error()
	} else {
		response.Healthy = true
	}

	writer.Header().Set("Content-Type", "application/json")
	json.NewEncoder(writer).Encode(response)
}

// validateOpenListConfig 验证表单中的 OpenList 配置是否可用于云上传。
func validateOpenListConfig(writer http.ResponseWriter, r *http.Request) {
	var req OpenListValidateRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJsonWithStatusCode(writer, http.StatusBadRequest, commonResp{
			ErrNo:  http.StatusBadRequest,
			ErrMsg: "无效的JSON格式: " + err.Error(),
		})
		return
	}

	cfg := configs.GetCurrentConfig()
	endpoint := strings.TrimRight(strings.TrimSpace(req.ExternalURL), "/")
	token := strings.TrimSpace(req.ExternalToken)
	storageName := strings.Trim(strings.TrimSpace(req.StorageName), "/")
	external := endpoint != ""

	if endpoint == "" {
		if globalOpenListManager != nil {
			endpoint = globalOpenListManager.GetAPIEndpoint()
			token = globalOpenListManager.GetAPIToken()
			external = globalOpenListManager.IsExternal()
		} else if cfg != nil {
			port := cfg.OpenList.Port
			if port == 0 {
				port = 5244
			}
			endpoint = fmt.Sprintf("http://127.0.0.1:%d", port)
		}
	}

	response := OpenListValidateResponse{
		External: external,
		Endpoint: endpoint,
		Storages: []openlist.StorageInfo{},
		Errors:   []string{},
	}

	if endpoint == "" {
		response.Errors = append(response.Errors, "OpenList 地址为空")
		response.Message = "请先填写外部 OpenList 地址，或启动内置 OpenList。"
		writeJSON(writer, response)
		return
	}

	client := openlist.NewClient(endpoint, token)
	ctx, cancel := context.WithTimeout(r.Context(), 10*time.Second)
	defer cancel()

	response.ServiceReady = client.IsServiceReady(ctx)
	if !response.ServiceReady {
		response.Errors = append(response.Errors, "无法访问 OpenList 服务，请检查地址、端口或网络。")
		response.Message = "OpenList 服务不可访问。"
		writeJSON(writer, response)
		return
	}

	if token == "" {
		response.OK = true
		response.Message = "OpenList 服务可访问，但未填写 Token，无法验证存储列表和上传权限。"
		writeJSON(writer, response)
		return
	}

	response.AuthChecked = true
	storages, err := client.ListStorages(ctx)
	if err != nil {
		response.Errors = append(response.Errors, "Token 验证或存储列表读取失败: "+err.Error())
		response.Message = "OpenList 可访问，但 Token 或管理权限验证失败。"
		writeJSON(writer, response)
		return
	}
	response.AuthOK = true
	response.Storages = storages

	if storageName != "" {
		response.StorageChecked = true
		if err := client.CheckStorageHealth(ctx, storageName); err != nil {
			response.Errors = append(response.Errors, "存储不可用: "+err.Error())
			response.Message = "连接和 Token 有效，但指定存储不可用。"
			writeJSON(writer, response)
			return
		}
		response.StorageOK = true
	}

	response.OK = true
	response.ReadyForUpload = response.ServiceReady && response.AuthOK && response.StorageChecked && response.StorageOK
	if response.ReadyForUpload {
		response.Message = "验证通过，可用于云上传。"
	} else if storageName == "" {
		response.Message = "连接和 Token 有效；填写存储位置后可继续验证上传目标。"
	} else {
		response.Message = "连接验证通过。"
	}

	writeJSON(writer, response)
}
