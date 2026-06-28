package stages

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"slices"
	"sync"
	"testing"

	"github.com/bililive-go/bililive-go/src/pipeline"
	"github.com/bililive-go/bililive-go/src/pkg/livelogger"
	"github.com/sirupsen/logrus"
)

type fakeOpenListProvider struct {
	endpoint string
	token    string
	running  bool
}

func (p fakeOpenListProvider) IsRunning() bool {
	return p.running
}

func (p fakeOpenListProvider) GetAPIEndpoint() string {
	return p.endpoint
}

func (p fakeOpenListProvider) GetAPIToken() string {
	return p.token
}

func TestCloudUploadStageUploadsToOpenList(t *testing.T) {
	var mu sync.Mutex
	var mkdirs []string
	var uploads []string
	var uploadedBody string

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "test-token" {
			t.Errorf("Authorization = %q, want test-token", r.Header.Get("Authorization"))
		}

		switch r.URL.Path {
		case "/api/fs/mkdir":
			var req struct {
				Path string `json:"path"`
			}
			if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
				t.Errorf("decode mkdir request: %v", err)
				http.Error(w, err.Error(), http.StatusBadRequest)
				return
			}
			mu.Lock()
			mkdirs = append(mkdirs, req.Path)
			mu.Unlock()
			_ = json.NewEncoder(w).Encode(map[string]any{"code": 200, "message": "success"})

		case "/api/fs/put":
			remotePath, err := url.PathUnescape(r.Header.Get("File-Path"))
			if err != nil {
				t.Errorf("decode File-Path: %v", err)
				http.Error(w, err.Error(), http.StatusBadRequest)
				return
			}
			body, err := io.ReadAll(r.Body)
			if err != nil {
				t.Errorf("read upload body: %v", err)
				http.Error(w, err.Error(), http.StatusBadRequest)
				return
			}
			mu.Lock()
			uploads = append(uploads, remotePath)
			uploadedBody = string(body)
			mu.Unlock()
			_ = json.NewEncoder(w).Encode(map[string]any{"code": 200, "message": "success"})

		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	SetOpenListProvider(fakeOpenListProvider{
		endpoint: server.URL,
		token:    "test-token",
		running:  true,
	})
	t.Cleanup(func() { SetOpenListProvider(nil) })

	tempDir := t.TempDir()
	videoPath := filepath.Join(tempDir, "video.mkv")
	if err := os.WriteFile(videoPath, []byte("video-data"), 0644); err != nil {
		t.Fatal(err)
	}

	stage, err := NewCloudUploadStage(pipeline.StageConfig{
		Name: pipeline.StageNameCloudUpload,
		Options: map[string]any{
			pipeline.OptionStorage:            "/本地存储/迅雷下载/直播录制",
			pipeline.OptionAdditionalStorages: []string{"/备份盘/直播录制", "/本地存储/迅雷下载/直播录制"},
			pipeline.OptionPathTemplate:       "/录播归档/{{ .Platform }}/{{ .HostName }}/{{ .RoomName }}/{{ .FileName }}",
			pipeline.OptionDeleteAfter:        false,
			pipeline.OptionFileTypes:          []string{"video"},
		},
	})
	if err != nil {
		t.Fatal(err)
	}

	ctx := &pipeline.PipelineContext{
		Ctx: context.Background(),
		RecordInfo: pipeline.RecordInfo{
			Platform: "BoyFriend",
			HostName: "主播",
			RoomName: "房间",
		},
		Logger: livelogger.New(livelogger.DefaultBufferSize, logrus.Fields{}),
	}

	output, err := stage.Execute(ctx, []pipeline.FileInfo{pipeline.NewVideoFileInfo(videoPath)})
	if err != nil {
		t.Fatal(err)
	}

	if len(output) != 1 || output[0].Path != videoPath {
		t.Fatalf("output = %#v, want original video file", output)
	}
	if uploadedBody != "video-data" {
		t.Fatalf("uploaded body = %q, want video-data", uploadedBody)
	}

	expectedPrimary := "/本地存储/迅雷下载/直播录制/录播归档/BoyFriend/主播/房间/video.mkv"
	expectedBackup := "/备份盘/直播录制/录播归档/BoyFriend/主播/房间/video.mkv"
	if !slices.Contains(uploads, expectedPrimary) {
		t.Fatalf("uploads = %#v, missing %s", uploads, expectedPrimary)
	}
	if !slices.Contains(uploads, expectedBackup) {
		t.Fatalf("uploads = %#v, missing %s", uploads, expectedBackup)
	}
	if len(uploads) != 2 {
		t.Fatalf("uploads = %#v, want 2 deduplicated upload targets", uploads)
	}

	expectedParent := "/本地存储/迅雷下载/直播录制/录播归档/BoyFriend/主播/房间"
	if !slices.Contains(mkdirs, expectedParent) {
		t.Fatalf("mkdirs = %#v, missing final parent %s", mkdirs, expectedParent)
	}
}

func TestNormalizeRemotePathRemovesWrappedTextareaNewlines(t *testing.T) {
	got := joinRemotePath(
		"/本地存储/迅雷下载/直播录制",
		"/录播归档/{{ .Platform }}/{{ .HostName }}\n/{{ .RoomName }}/{{ .FileName }}",
	)
	want := "/本地存储/迅雷下载/直播录制/录播归档/{{ .Platform }}/{{ .HostName }}/{{ .RoomName }}/{{ .FileName }}"
	if got != want {
		t.Fatalf("joinRemotePath() = %q, want %q", got, want)
	}
}
